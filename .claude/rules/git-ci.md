# Git / CI

## Коммиты
- Conventional Commits: `feat(game): add quoridor wall validation`, `fix(auth): refresh token race`, `chore(deploy): bump livekit image`.
- Скоуп — имя сервиса/модуля (`game`, `auth`, `gateway`, `web`, `app`, `deploy`).
- Один коммит — одна логическая единица изменения; не смешивать рефактор с новой фичей в одном коммите.

## Ветки
- `main` — всегда деплоится, защищена от прямых пушей.
- Фиче-ветки: `feat/<scope>-<short-desc>` (`feat/game-quoridor-engine`), багфиксы: `fix/<scope>-<short-desc>`.
- Мерж в `main` — только через PR, squash-merge, чтобы история `main` оставалась линейной по фичам.

## Pull Request
- Обязательный шаблон: что сделано, как проверено (какие тесты — unit/integration/e2e), скриншот/видео для UI-изменений.
- PR должен проходить CI (lint + unit + integration/e2e) до мержа — без исключений даже для "мелких" фиксов.
- Один PR — один сервис/модуль по возможности; кросс-сервисные изменения (например, proto + все клиенты) — можно одним PR, но с чётким описанием, что задето.

## CI (GitHub Actions)
- Пайплайн шагов на каждый PR:
  1. `gofmt`/`golangci-lint`, `eslint`/`prettier` (web), `swiftlint` (app) — параллельно
  2. Unit-тесты — параллельно по сервисам
  3. Integration/E2E — поднятие стека через `docker-compose.e2e.yml`, гоняется после unit
  4. Build — сборка Docker-образов всех сервисов (проверка, что всё вообще собирается)
- Кэширование Go-модулей/npm-пакетов между запусками — обязательно, иначе CI будет неприемлемо медленным на монорепо.
- Секреты CI (даже тестовые токены LiveKit/OAuth) — только через GitHub Actions secrets, никогда в `.yml` файле пайплайна напрямую.
