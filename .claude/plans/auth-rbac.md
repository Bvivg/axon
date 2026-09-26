# RBAC: роли и доступы

## Контекст

Нужна система ролей и доступов: у пользователя несколько ролей, у роли —
несколько доступов (permissions). Доступ живёт в самоссылающейся таблице
(`parent_id` на эту же таблицу) и принадлежит неймспейсу (`user`, `chat`,
`game`, `calling`, `session`, `rbac`), у каждого неймспейса — как минимум
`read`/`mutate`, плюс возможность произвольного кастомного action.

RBAC — часть identity, поэтому живёт в `auth` сервисе, в схеме `auth`, тем же
способом, что и остальные identity-таблицы (`users`, `refresh_tokens`).

Результатом этого этапа считается сама система (модель данных + доступ к ней
через API), а не включение проверок на существующих ручках (`chat`/`game`/
`calling` пока ничего не знают про permissions — это отдельная, более крупная
задача с более рискованным blast radius, вне этого этапа).

## Модель данных

Миграция `000003_rbac` в `core/services/auth/migrations/`:

```
permissions (id, namespace, action, parent_id -> permissions.id)
roles       (id, name)
role_permissions (role_id -> roles, permission_id -> permissions)
user_roles       (user_id -> users, role_id -> roles)
```

`parent_id` — иерархия внутри доступа: у каждого неймспейса есть корневой
permission `manage` (`parent_id IS NULL`), а `read`/`mutate` (и любой
кастомный action, например `game:moderate`) ссылаются на него как на
родителя. Обладание родительским permission неявно даёт все дочерние —
проверка идёт through recursive CTE по цепочке `parent_id` вверх от целевого
permission, пересекая её с permissions, которые есть у пользователя через его
роли.

Сиды (той же миграцией): роли `user` (id
`00000000-0000-0000-0000-000000000001`) и `admin` (`...002`) — фиксированные
UUID, чтобы Go-код мог ссылаться на них константами без запроса "роль по
имени". `user` получает `manage` для `user`/`session`/`chat`/`game`/`calling`
(всё, кроме `rbac` — управление ролями не подразумевается по умолчанию).
`admin` получает `manage` для всех неймспейсов, включая `rbac`.

Роль `user` назначается автоматически при регистрации (password и OAuth) —
внутри той же транзакции, что создаёт запись в `users`
(`repository/composite.go`). Роль `admin` никому не назначается миграцией:
первый админ выдаётся вручную через SQL против прод-базы — это осознанный
выбор (нет проверяемого способа отличить "первого администратора" от обычной
регистрации), задокументирован здесь, а не в коде.

## Проверка доступа

`Repository.UserHasPermission(ctx, userID, namespace, action) (bool, error)`
— один SQL-запрос (recursive CTE вверх по `parent_id` от целевого permission,
пересечение с ролями пользователя). Без кеша/in-memory дерева: используется
только на редких admin-операциях (`AssignRole`/`RevokeRole`/`ListRoles`), не
на горячем пути, так что накладные расходы одного запроса не имеют значения.

Это сознательно НЕ то же самое, что верификация JWT (та остаётся локальной,
через JWKS, без похода в auth — не трогаем). RBAC-проверки идут в сам auth
сервис, а не через gateway policy — это его собственные данные.

## API (`auth.proto`)

- `User.roles` — `repeated string roles = 11` (имена ролей); заполняется в
  `Register`/`Login`/`CompleteOAuth`/`GetMe`/`UpdateProfile` через
  `Service.attachRoles` (best-effort, деградирует молча при ошибке — тот же
  паттерн, что presence в `ListSessions`).
- `ListRoles(ListRolesRequest) returns (ListRolesResponse{repeated Role roles})`
  — каталог ролей с их permissions. Требует `rbac:read`.
- `AssignRole(AssignRoleRequest{user_id, role_id}) returns (AssignRoleResponse{})`
  — идемпотентно (`ON CONFLICT DO NOTHING`). Требует `rbac:mutate`.
- `RevokeRole(RevokeRoleRequest{user_id, role_id}) returns (RevokeRoleResponse{})`
  — `ErrRoleNotFound`, если пары не было. Требует `rbac:mutate`.

Новые сообщения: `Permission{id, namespace, action, optional parent_id}`,
`Role{id, name, repeated Permission permissions}`.

Gateway (`policy.go`): все три — `Public: false`; `AssignRole`/`RevokeRole` —
`TierSensitive` (мутация прав), `ListRoles` — `TierStandard`.

## Файлы

- `core/services/auth/migrations/000003_rbac.up.sql` / `.down.sql`
- `core/services/auth/internal/domain/rbac.go` — `Role`, `Permission`,
  `RoleUser`/`RoleAdmin` константы
- `core/services/auth/internal/domain/errors.go` — `ErrRoleNotFound`,
  `ErrPermissionDenied`
- `core/services/auth/internal/repository/rbac.go` — `ListRoles`,
  `RoleNamesForUser`, `AssignRole`, `RevokeRole`, `UserHasPermission`
- `core/services/auth/internal/repository/composite.go` — назначение роли
  `user` внутри `CreateUserWithPassword`/`CreateUserWithOauthAccount`
- `core/services/auth/internal/service/service.go` — новые методы `Store`
- `core/services/auth/internal/service/rbac.go` — `ListRoles`, `AssignRole`,
  `RevokeRole`, `requirePermission`, `attachRoles`
- `core/services/auth/internal/service/{password_flow,oauth_flow,profile}.go`
  — `attachRoles` перед возвратом `User`
- `core/shared/proto/axon/auth/v1/auth.proto`, регенерация через `buf
  generate` (в Docker, как обычно)
- `core/services/auth/internal/server/convert.go`, `handler.go` — конверсия
  и три новых RPC-хендлера
- `core/services/gateway/internal/policy/policy.go` — три новых правила
- `core/services/auth/internal/service/store_fake_test.go`,
  `harness_test.go`, новый `rbac_test.go` (unit, fake store)
- `core/services/auth/integration/rbac_test.go` (testcontainers, реальный
  Postgres — recursive CTE, transaction rollback)
- `core/services/auth/e2e/rbac_test.go` + `core/deploy/docker-compose.e2e.yml`
  (`POSTGRES_DSN` для контейнера `e2e`, чтобы тест мог напрямую выдать себе
  `admin` для проверки admin-веток — единственное точечное использование
  прямого доступа к БД в e2e, по аналогии с `stageTestImageInMinio`)

## Проверка

- `go test ./...` в `core/services/auth` (unit)
- `docker compose run --rm auth-test` / `make test-integration` (recursive
  CTE и транзакционное назначение роли на реальном Postgres)
- `make test-e2e` (полный цикл: регистрация → `user` в `User.roles` →
  выдача `admin` напрямую в БД → `ListRoles`/`AssignRole`/`RevokeRole` через
  gateway → `PermissionDenied` для не-админа)
- `golangci-lint run` / `buf lint`

## Критерий готовности

Пользователь при регистрации получает роль `user`; `GetMe` показывает её;
`admin`, назначенный напрямую в БД, может листать роли и назначать/отзывать
их у других пользователей через gateway; обычный пользователь получает
`PermissionDenied` на тех же ручках — всё подтверждено e2e.
