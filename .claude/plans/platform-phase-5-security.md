# Фаза 5 — безопасность уровня production

## Контекст
Внутренние вызовы (gateway → auth/chat/game, chat → auth, game ↔ game, загрузка JWKS) идут
по открытому HTTP внутри сети `app`: любой контейнер в этой сети может вызвать любой сервис.
Ключ подписи JWT один и статичный, его смена — ручная правка `.env` и рестарт. Доступ к
публичным RPC уже задан таблицей `gateway/internal/policy` (неизвестная процедура закрыта,
тест сверяет таблицу с контрактом), роли проверяет auth — это остаётся. Событий
безопасности (вход, отзыв сессий, смена ролей) нет нигде, кроме логов. В CI есть `govulncheck` и `npm audit`, но образы не сканируются, SBOM и
подписи нет.

Результат: сервис без валидного клиентского сертификата не может вызвать другой сервис
(проверяет e2e); ключи JWT ротируются сами с двумя опубликованными `kid`, без отказов под
нагрузкой; события безопасности уходят append-only топиком; образы сканируются,
получают SBOM и подпись.

## Решение
- **mTLS** — `shared/pkg/mtls`: сертификат, ключ и CA читаются из файлов и перечитываются
  при смене (сертификаты живут 1 ч), сервер требует клиентский сертификат и пускает только
  имена из `MTLS_ALLOWED_CLIENTS`; клиентский транспорт предъявляет свой. Без `MTLS_*`
  сервис работает по HTTP, как раньше (локальный `start:dev`). Внутренние listener'ы
  auth/chat/game — только TLS; `/healthz`, `/readyz`, `/metrics` вынесены на admin-порт
  (у game он появляется впервые). Публичный вход gateway остаётся за Envoy/TLS-фронтом.
- **CA** — `step-ca` (smallstep) в overlay `docker-compose.mtls.yml`, job `certs` выпускает
  сертификат каждому сервису в его собственный том и продлевает (`step ca renew --daemon`).
  Короткий срок жизни + перечитывание файлов = ротация без рестартов.
- **Ротация JWKS** — ключи в `auth.signing_keys` (приватный ключ зашифрован AES-GCM ключом
  `JWT_KEY_ENCRYPTION_KEY`). Раз в `JWT_ROTATION_INTERVAL` один инстанс auth под advisory
  lock создаёт новый ключ с `activates_at = now + JWT_ROTATION_LEAD`: он сразу публикуется в
  JWKS, а подписывать им начинают после того, как верификаторы его подтянули. Старый ключ
  уходит из JWKS через `access TTL` после активации нового. Ключ из env — только стартовый.
  `JWT_ROTATION_INTERVAL=0` — статичные ключи, как сейчас.
- **Аудит** — auth пишет `auth.audit.v1` (вход/неудачный вход, выход, отзыв сессий, повтор
  refresh-токена, смена роли; смены пароля в продукте нет) через outbox фазы 2 в той же транзакции; топик с `retention.ms=-1`.
- **CI** — к существующим `govulncheck` и `npm audit`: Trivy по каждому образу (HIGH/CRITICAL
  с исправлением — ошибка), SBOM (syft, SPDX) артефактом, на push в `main` — публикация в
  GHCR и keyless-подпись cosign (OIDC GitHub Actions) с приложенным SBOM.

## Файлы
- `core/shared/pkg/mtls/`, main всех сервисов, `shared/pkg/authn` (транспорт JWKS).
- `core/deploy/docker-compose.mtls.yml`, `core/deploy/mtls/`.
- `core/services/auth/internal/jwt/` (кольцо ключей), `internal/keyring/`, миграция
  `signing_keys`, `cmd/auth/main.go`.
- `core/services/auth` — аудит через outbox.
- `.github/workflows/ci.yml`, `core/deploy/docker-compose.tools.yml` (trivy, syft).

## Как проверяется
- Unit: перечитывание сертификата, отказ клиенту без сертификата / с чужим CA / с именем
  вне списка; кольцо ключей (публикация до активации, вывод из JWKS после TTL); аудит — строка outbox в транзакции входа.
- E2E (`docker-compose.mtls.yml`): контейнер без сертификата и с сертификатом `intruder`
  не может вызвать auth/chat/game; gateway может; полный сценарий входа и партии зелёный.
- Ротация под нагрузкой: интервал 40 с, lead 10 с, TTL токена 30 с; клиент 3 минуты ходит
  через gateway и обновляет токены — 0 отказов `unauthenticated`, видно ≥ 3 разных `kid`.
- `make lint`, `go test -race ./...`, Trivy/syft локально через tools-проект.

## Критерий готовности
Сервис без валидного клиентского сертификата не может вызвать другой сервис, и это
проверяет e2e.

## Результаты (2026-11-20)
- **mTLS.** auth, chat, game и rating слушают внутренний порт только по TLS 1.3 и пускают имена из
  `MTLS_ALLOWED_CLIENTS` (auth: gateway, chat, game; chat: gateway; game: gateway, game; rating:
  gateway). JWKS auth отдаёт и на внутреннем порту, верификаторы ходят туда по mTLS. Game вынес
  health/metrics на `GAME_ADMIN_ADDR` и общается с соседями по `https`/`wss` с именем `game`.
  WebSocket-клиенты идут по HTTP/1.1 (`mtls.HTTP1Only`), RPC — по h2.
- **step-ca** (`smallstep/step-ca:0.28.3`) + job `certs` (`step-cli`): сертификат каждому сервису в
  его собственный том, продление при остатке меньше `MTLS_RENEW_BEFORE`; ключ и сертификат
  подменяются через `mv`, сервисы перечитывают файлы по mtime (`MTLS_RELOAD_INTERVAL`).
  Минимальный срок у step-ca — 5 минут, поэтому в e2e сертификат живёт 6 минут.
- **Ротация JWKS**: lead по умолчанию 20 минут — больше самого длинного обновления JWKS у
  верификаторов (chat — 15 минут). Под advisory lock четыре инстанса создают один ключ за
  интервал (integration-тест); симуляция 6 часов с часовым интервалом — каждый живой токен
  проверяется на каждом шаге.
- **E2E** (`make test-e2e-security`, проект `axon-security-e2e` на base + `docker-compose.mtls.yml`
  + `docker-compose.mtls-e2e.yml`):
  - plain HTTP, TLS без клиентского сертификата и сертификат `intruder` того же CA отклонены
    auth, chat, game и rating (`certificate required` / `bad certificate`);
  - через gateway 2 минуты работают auth, chat и game, WebSocket партии доходит до владельца по wss;
  - auth предъявил продлённый сертификат без рестарта;
  - ротация под нагрузкой (интервал 45 с, lead 15 с, TTL токена 5 мин, 8 потоков, 3 минуты):
    81 310 вызовов `GetMe`, 0 отказов, 4 ключа подписи.
- **CI**: Trivy по образам auth/chat/gateway/game/rating — 0 исправимых HIGH/CRITICAL; SBOM (syft, SPDX)
  артефактом; на push в `main` — GHCR + `cosign sign` и `cosign attest` с SBOM (keyless).
- Не проверено: публикация и подпись в GHCR (только на push в `main`); mTLS несовместим с
  overlay SFU (вебхуки LiveKit на chat без сертификата) и с observability (Prometheus скрейпит
  game по 8083) — эти overlay с mTLS не объединялись.
