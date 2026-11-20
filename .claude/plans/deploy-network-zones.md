# Deploy — сети по зонам ответственности

## Контекст
Весь стек Axon живёт в одной сети `axon_default`: web может открыть соединение с Postgres,
LiveKit — с Kafka, gateway — с базой. Это противоречит `rules/security.md` (gateway — граница
доверия) и ничем не похоже на будущие NetworkPolicy в k8s (фаза 6). Заодно стек по умолчанию
тяжёлый: три брокера Kafka и два game на ноутбуке нужны только для проверки отказоустойчивости,
а журнал game — источник истины с фазы 1 — лежит на эфемерном диске брокеров без тома.

Результат: каждый контейнер подключён только к сетям своей зоны, базовый стек лёгкий
(1 брокер, 1 game), режим отказоустойчивости включается отдельным overlay, журнал переживает
пересоздание брокеров.

## Решение
Один compose-проект `axon`, несколько сетей:

| сеть | кто | зачем |
|---|---|---|
| `edge` | calls-https, web, gateway, auth, minio | всё, что достижимо с входа: TLS-фронт отдаёт web, `/api` → gateway, `/storage` → minio, тестовый OAuth → auth |
| `app` | gateway, auth, chat, game | вызовы между сервисами (Connect, JWKS, пересылка к владельцу партии) |
| `data` | auth, chat, game, postgres, redis, kafka*, minio, minio-init, game-migrate, migrate, snapshot | хранилища видят только сервисы-владельцы и джобы |
| `media` | chat, gateway, livekit, coturn | звонки: chat управляет LiveKit и принимает вебхуки, gateway проксирует сигналинг |
| `lan` | calls-dns, calls-check, tailnet DNS | доступ к стенду с телефона, ни с кем внутри не общаются |

Ключевые свойства: web и TLS-фронт не видят Postgres/Redis/Kafka; gateway не видит хранилища;
LiveKit не видит данные.

Отказоустойчивость — overlay `docker-compose.ha.yml`: три брокера (RF 3, min ISR 2) и `game` ×2.
Базовый стек — один брокер (RF 1), `game` ×1. Отдельный overlay, а не profile: кворум KRaft
одного брокера и трёх различается конфигом первого брокера, profile этого не выражает.

У каждого брокера — именованный том данных (`KAFKA_LOG_DIRS=/var/lib/kafka/data`). У HA-режима свои тома
`kafka-ha-1..3`: кворум из одного и из трёх узлов несовместим на одних данных, отдельные тома
позволяют переключать режимы без ручной чистки.

Тестовые стеки (`docker-compose.e2e.yml`, `docker-compose.web-e2e.yml` и их calls-overlay)
остаются одной сетью: это одноразовые проекты одного прогона, изоляцию проверяет стенд.

## Файлы
- `core/deploy/docker-compose.yml` — сети `edge/app/data/media/lan`, `networks:` у каждого
  сервиса, один брокер с томом `kafka-data`, `game` ×1, RF 1.
- `core/deploy/docker-compose.ha.yml` — новый: kafka-2/3 с томами, кворум из трёх, RF 3, `game` ×2.
- `core/deploy/docker-compose.calls-lan.yml`, `docker-compose.calls-sfu-lan.yml` — сети для
  добавляемых сервисов. `docker-compose.backup.yml` — самостоятельный стенд учений по
  восстановлению, а не overlay, и остаётся одной сетью.
- `.github/workflows/ci.yml` — `config -q` для базового файла вместе с `ha` overlay.
- `core/services/game/cmd/game/main.go` — значения по умолчанию RF 1 / min ISR 1.
- `scripts/start-dev.mjs` — инфраструктура: один брокер; `livekit-bridge` поднимается как compose-сервис
  (profile `dev`, сеть `media`, `docker-compose.calls-sfu-lan.yml`) вместо `docker run` в сеть `axon_default`,
  которой больше нет.
- `.claude/rules/infra.md` — правило «новое делим по ответственности», контейнеры только через Compose,
  одноразовые проекты `axon-<назначение>` убираются после прогона, абсолютные пути bind-mount в overlay вне `core/deploy`.

## Перевод стенда
Журнал на стенде лежал в `/tmp` внутри контейнеров брокеров, поэтому переход на тома его теряет.
Перед стартом новых брокеров `game` останавливается и `game.projector_offsets.next_offset`
сбрасывается в 0, иначе проектор пропустил бы события нового топика. Read model в Postgres
остаётся, открытые партии владелец поднимает из него при claim.

## Как проверяется
- `docker compose config -q` для всех файлов, как в CI.
- Стенд: изнутри `web` и `calls-https` — `postgres:5432`, `redis:6379`, `kafka:29092` не
  резолвятся; из `gateway` — тоже; из `game` — резолвятся; из `livekit` — Postgres недоступен.
- Стенд с `ha` overlay: 3 брокера, game ×2; пересоздание брокеров не теряет журнал
  (offset топика после `up --force-recreate kafka` тот же).
- Web e2e chess/quoridor/tictactoe на web-e2e стеке зелёные; axon.test открывается, партия
  играется, звонок 1:1 проходит.

## Критерий готовности
На стенде web и gateway физически не могут подключиться к Postgres, а все e2e и axon.test работают.
