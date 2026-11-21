# Фаза 6 — Kubernetes и GitOps

## Контекст
Весь стек поднимается только через Docker Compose: масштабирование — overlay `docker-compose.ha.yml`
с фиксированным числом реплик, сбои — Toxiproxy и `docker restart`, деплой — ручной `compose up`.
Нет ни автоскейлинга по нагрузке, ни защиты от одновременного вывода реплик, ни декларативного
деплоя из git. Фаза переносит тот же стек в Kubernetes, не меняя код сервисов: всё, что нужно
кластеру (адрес пира game, health, mTLS-файлы, ротация JWT), уже настраивается через env.

Результат: `make kind-up` с нуля поднимает кластер, Argo CD из git разворачивает операторы,
инфраструктуру и сервисы и показывает все приложения Synced/Healthy; game масштабируется по
активным партиям; chaos-сценарии фазы 3 проходят в кластере без потерянных ходов.

## Решение
- **Кластер** — kind `axon`: control-plane + 3 worker с метками
  `topology.kubernetes.io/zone=a|b|c`. Образы собираются на хосте (контекст `core/`) и грузятся
  `kind load docker-image` только в worker'ы (на control-plane ничего не планируется, а диск Docker
  общий со стеками `axon`), тег `local`, `imagePullPolicy: Never`; registry-контейнер не нужен.
  Наружу через `extraPortMappings` на NodePort: gateway `127.0.0.1:28080`, web `:23003`,
  Argo CD `:28443` — не пересекаются со стеками `axon` и e2e.
- **GitOps** — `make kind-up` ставит только Argo CD (чарт `argo-cd` 10.18.2, Argo CD v3.6.2) и
  корневой Application `core/deploy/k8s/argocd/root.yaml`. Root рендерит чарт
  `core/deploy/k8s/argocd/apps` (app-of-apps): операторы → `platform` (CA) → окружение `axon` →
  ApplicationSet превью. Окружение — чарт `charts/stack`: Namespace и по Application на
  компонент в sync waves `secrets(0) → infra(1) → auth, rating(2) → chat, game(3) → gateway,
  cdc(4) → web(5)`; миграции — PreSync-хук в приложении сервиса, то есть после готовой
  инфраструктуры. Чтобы волна ждала дочернее приложение, в `argocd-cm` добавлена health-проверка
  `argoproj.io/Application` (Healthy только при Synced) и `RedisReplication` (есть master).
  Репозиторий `Bvivg/axon` публичный, ревизия — текущая ветка (`ARGO_REVISION`, по умолчанию
  `git rev-parse --abbrev-ref HEAD`; `kind-up` падает, если ветки нет на origin).
- **Операторы** (upstream-чарты, версии в `argocd/apps/values.yaml`): cert-manager v1.21.3,
  CloudNativePG 0.29.2 (оператор 1.30.2), Strimzi 1.3.1 (API `kafka.strimzi.io/v1`, Kafka
  4.3.2), Redis Operator 0.27.0, kube-prometheus-stack 93.6.0 только с Prometheus Operator и
  Prometheus (без Alertmanager, Grafana, node-exporter, kube-state-metrics, правил), prometheus-
  adapter 5.4.2, Chaos Mesh 2.8.8 (containerd, сокет kind, webhook через cert-manager, без
  dashboard). Все — `ServerSideApply=true`: без этого CRD kube-prometheus-stack не влезают в
  аннотацию, а внедрённые `caBundle` дают вечный OutOfSync.
- **Инфраструктура** — чарт `charts/infra`:
  - CNPG `Cluster` `axon-pg`: 3 инстанса (`postgresql:17.10-minimal-trixie`), anti-affinity по
    зоне, `wal_level=logical`, managed roles `<сервис>_service` и `debezium` (replication),
    пароли из секретов; схемы и `pg_trgm` — Sync-хук Job, который запускает те же
    `core/deploy/postgres/init/*` (symlink в `files/`) на том же образе CNPG, что уже лежит на
    узлах (`postgres:17-alpine` тянулся с Docker Hub 3–9 минут). `axon-pg-rw` — запись,
    `axon-pg-ro` — `GAME_READ_DATABASE_URL`. При ≥2 инстансах — синхронизация логических слотов
    (`replicationSlots.highAvailability.synchronizeLogicalDecoding`, `sync_replication_slots`,
    `hot_standby_feedback`), а коннектор создаёт слот с `slot.failover=true`: без этого после
    failover слот `axon_outbox` пропадал и задача Debezium падала. `livenessProbeTimeout: 60` —
    на перегруженном хосте 30 с без ответа уже давали лишний failover.
  - Strimzi: `KafkaNodePool` ×3 KRaft (dual-role, rack по зоне), RF=3, `min.insync.replicas=2`;
    `KafkaTopic` для журнала и лизов game (24 партиции) и для топиков группы `forever`, которые
    рендерятся из `topic.creation.*` в `outbox-connector.json` (сейчас `auth.audit.v1`,
    `game.finished.v1`, `rating.changed.v1`, `retention.ms=-1`); `KafkaConnect` на образе
    `Dockerfile.connect` (Strimzi Kafka + плагин Postgres из `debezium/connect:3.7.0.Final`).
    Остальные outbox-топики создаёт Connect по тем же правилам, что в compose.
  - Redis Operator: `RedisReplication` `app-redis` ×3 со встроенным sentinel ×3 (в 0.27
    sentinel — поле `RedisReplication`, отдельный `RedisSentinel` не нужен) и одиночный `Redis`
    `edge-redis` (64 МБ, `volatile-ttl`).
  - MinIO — Deployment + PVC + Sync-хук создания бакетов; Apicurio Registry — Deployment со
    startup-пробой до 5 минут и 100m CPU: JVM на занятом узле не успевала за liveness.
- **CDC** — чарт `charts/cdc`, волна после сервисов, потому что публикация требует outbox-таблиц
  из миграций: Job `cdc-setup` (тот же `core/deploy/cdc/setup.sh`), `KafkaConnector`
  `axon-outbox`, чей `config` целиком рендерится из `core/deploy/cdc/outbox-connector.json`
  (`connector.class`/`tasks.max` уходят в `spec`; поверх — `connector.config` из values чарта,
  сейчас только `slot.failover`), и Job регистрации `events.proto` в Apicurio.
- **Секреты** — чарт `charts/secrets-bootstrap`: ServiceAccount и Role на `get/create secrets`
  (волна −1), затем Sync-хук Job на образе `toolbox` (kubectl + openssl) создаёт недостающие
  секреты: `pg-<роль>` и `pg-debezium` (basic-auth для CNPG), `redis-app`, `redis-edge`, `minio`,
  `auth-jwt` (стартовый RSA-ключ и `JWT_KEY_ENCRYPTION_KEY`), `chat-vapid`. Существующие не
  трогает, поэтому повторный sync не меняет ключи. В git секретов нет. Так же в preview.
- **Сервисы** — library chart `charts/axon-service` (Deployment, Service, PDB, HPA,
  ServiceMonitor, Certificate, PreSync-миграции из ConfigMap, startup/liveness/readiness на
  admin-порт, `topologySpreadConstraints` по zone и hostname, non-root, read-only root, drop ALL)
  и тонкий чарт на сервис: `auth`, `chat`, `game`, `gateway`, `rating`, `web`. Значения окружения
  (`values-kind.yaml`, `values-preview.yaml`) передаются каждому чарту целиком: общий раздел
  `global` и раздел с именем чарта поверх его `values.yaml`. SQL миграций читается из
  `core/services/<svc>/migrations` через symlink.
  - game: `GAME_ADVERTISE_ADDR=$(POD_IP):8083`, `GAME_INSTANCE_ID` — имя пода; Deployment, не
    StatefulSet: владение партиями решают лизы. `GAME_TEST_HOOKS=true` только в kind — им
    пользуется нагрузчик; gateway этот заголовок не пробрасывает, а до game без сертификата не
    достучаться.
  - HPA game по `game_active_sessions` (Pods-метрика, сумма по `game` на под, через
    prometheus-adapter), min 2 / max 6, `averageValue` 50, scale-down окно 60 с. Пока партий нет,
    у `game_active_sessions` нет ни одной серии, поэтому правило adapter'а находит поды по
    `game_partitions_owned` и отдаёт `sum(game_active_sessions) or 0`: иначе HPA без метрики —
    Degraded, а с ним `axon-game`, `axon` и `root`.
  - PDB `minAvailable: 1` у всех сервисов с ≥2 репликами (auth, chat, gateway, game, rating).
  - mTLS: `platform` создаёт self-signed CA и `ClusterIssuer` `axon-internal`; `Certificate` на
    сервис (ECDSA, 1 ч, `renewBefore` 20 мин, SAN `<svc>`, `<svc>.<ns>.svc`), секрет монтируется в
    `/etc/axon/tls`; списки клиентов как в `docker-compose.mtls.yml`.
  - gateway в kind: `TRUSTED_PROXIES=1`, лимиты 600/60 в минуту и origin `localhost:3000` в CORS —
    как в `docker-compose.e2e.yml`, иначе e2e auth/chat не различают вызывающих. Сервисы в kind
    идут с `APP_ENV=test`, как в e2e-стеке, поэтому refresh-cookie — `Secure` (localhost браузеры
    считают безопасным); preview остаётся `development`.
  - Calls/LiveKit/coturn в кластер не переносятся (`CALLS_ENABLED=false`).
- **Preview на PR** — ApplicationSet `axon-preview` с генератором Pull Request (GitHub,
  `Bvivg/axon`, метка `preview`, `requeueAfterSeconds: 300`, без токена): Application
  `axon-pr-<номер>` на чарт `stack` с ревизией head SHA, namespace `axon-pr-<номер>`,
  `values-preview.yaml` (по одной реплике, Postgres 1 инстанс и чтение с primary, Kafka 1 брокер,
  RF 1, без sentinel и Apicurio, HPA/PDB выключены, test hooks выключены). Снятие метки или
  закрытие PR удаляет Application, а с ним по финализатору дочерние приложения и Namespace.
- **Тестовые прогоны в кластере** — образ `Dockerfile.toolbox`: собранные `go test -c` сьюты
  auth, chat, rating (вместе с `chain_test.go`) и `shared/e2e/security`, `loadgen`, psql, kcat,
  kubectl. Внутренние API требуют клиентский сертификат, поэтому Job'ы в `core/deploy/k8s/jobs/`
  идут через envoy-sidecar (TCP → mTLS, сертификат `toolbox-mtls` с CN `gateway`, проверка SAN
  сервера, TLS 1.3 и минимум, и максимум: у upstream-контекста envoy максимум по умолчанию 1.2). E2E идут изнутри кластера: rating e2e пишет прямо в Kafka и Postgres, mTLS e2e
  стучится во внутренние порты — с хоста этого не сделать.
- **Chaos Mesh** — `core/deploy/k8s/chaos/workflow.yaml`: `Workflow` с шагами
  `core/deploy/chaos/scenario.sh` (задержка 300 мс от primary Postgres 20 с, 100 % потерь от
  app-redis 10 с, pod-kill с grace 30 с game → chat → gateway → auth) плюс жёсткий kill брокера
  Kafka. Нагрузка — `loadgen` Job'ом прямо в game, сверка — та же, что в фазе 3
  (`toolbox/verify.sql`: ход, подтверждённый клиенту, есть в `game.moves`), после того как
  проектор догнал конец журнала.
- **CI** — job `k8s`: `make k8s-validate` — `helm lint` всех чартов и `kubeconform -strict` по
  отрендеренным манифестам `values-kind.yaml` и `values-preview.yaml`, корню и Job'ам; схемы CRD
  — каталог datreeio, для Chaos Mesh схемы генерируются из CRD закреплённой версии чарта.
- **Make** — `kind-up`, `kind-down`, `kind-load` (`IMAGES=` для подмножества), `kind-e2e`,
  `kind-chaos` (`CHAOS=0` — только нагрузка, для проверки HPA), `k8s-validate`.

## Файлы
- `core/deploy/k8s/kind/` — `cluster.yaml`, `up.sh`, `images.sh`, `run-job.sh`, `chaos.sh`
- `core/deploy/k8s/argocd/` — `values.yaml` (Argo CD), `root.yaml`, чарт `apps/`
- `core/deploy/k8s/charts/` — `axon-service`, `auth`, `chat`, `game`, `gateway`, `rating`, `web`,
  `infra`, `cdc`, `secrets-bootstrap`, `platform`, `stack`
- `core/deploy/k8s/values-kind.yaml`, `core/deploy/k8s/values-preview.yaml`
- `core/deploy/k8s/Dockerfile.connect`, `Dockerfile.toolbox`, `toolbox/`, `jobs/`, `chaos/`, `ci/validate.sh`
- `Makefile`, `.github/workflows/ci.yml`, `.gitignore`, `.claude/rules/infra.md` (раздел Kubernetes),
  `roadmap.md`

## Как проверяется
- `make kind-down && make kind-up` → exit 0; `kubectl -n argocd get applications` — все 19
  `Synced`/`Healthy`.
- `make kind-e2e` — Go-сьюты auth, chat, rating (включая рейтинговую партию через gateway →
  game → outbox → Connect → rating) и `shared/e2e/security` зелёные изнутри кластера против
  `gateway:8080`; с хоста `http://localhost:28080` и `http://localhost:23003` отвечают.
- mTLS: без сертификата, с сертификатом вне allow-list и с сертификатом чужого CA —
  отказ от auth/chat/game/rating (`security` + шаг `foreign-ca` в `kind-e2e`).
- HPA: `CHAOS=0 WAIT_SCALE_DOWN=1 make kind-chaos` — реплики game растут с 2 до ≥3, после
  нагрузки возвращаются к 2 (`WAIT_SCALE_DOWN=1` ждёт этого перед отчётом); сверка 0 потерянных
  подтверждённых ходов. Размер нагрузки — `JOB_ENV='{"MATCHES":"4000"}'`.
- PDB: одновременный `kubectl drain --pod-selector app.kubernetes.io/name=game` двух worker'ов с
  подами game — вторая эвикция ждёт PDB, Ready-подов game всегда ≥1.
- Chaos: `make kind-chaos` — workflow `Accomplished`, сверка: `completed_and_matching_db ==
  completed_by_client`, `sessions_with_acknowledged_moves_missing = 0`.
- Preview: метка `preview` на открытом PR → `axon-pr-<n>` Synced/Healthy; снятие метки →
  namespace удалён.
- `make k8s-validate` — 0 ошибок; `make kind-down` удаляет кластер.

## Критерий готовности
`make kind-up` поднимает весь стек, Argo CD зелёный, chaos-сценарии фазы 3 проходят в кластере.

## Результаты (2026-11-20)
- **Argo CD**: 19 приложений (root, 7 операторов, `platform`, окружение `axon` и 9 его
  компонентов) Synced/Healthy. До зелёного найдено и исправлено:
  - пока партий нет, у `game_active_sessions` нет серий → HPA game Degraded → `axon-game`, `axon`
    и `root` Degraded; root 40 минут ретраил синк старой ревизии и не доносил исправление
    adapter'а. Правило adapter'а теперь находит поды по `game_partitions_owned` и отдаёт 0;
  - `postgres:17-alpine` для Job'ов схем и CDC тянулся с Docker Hub 3,5–9 минут → образ CNPG,
    который уже лежит на узлах;
  - хост был перегружен (load average Docker VM до 128 на 10 CPU, параллельно шли чужие
    e2e-стеки): liveness CNPG увёл primary в failover, слот `axon_outbox` пропал, задача Debezium
    упала с `no longer available on the server`; Apicurio убивался liveness раньше старта JVM.
    Отсюда failover-слоты, `livenessProbeTimeout: 60`, startup-проба и 100m CPU у Apicurio;
  - envoy в Job'ах toolbox с `tls_minimum_protocol_version: TLSv1_3` без максимума отвечал
    `NO_SUPPORTED_VERSIONS_ENABLED` — все вызовы через sidecar рвались (OAuth в auth e2e, rating
    API, нагрузчик);
  - `APP_ENV=development` и `REFRESH_COOKIE_SECURE=false` в kind роняли
    `TestBrowserNeverReceivesTheRefreshTokenInTheBody` → `APP_ENV=test`, как в e2e-стеке.
- **`make kind-e2e`** (24 с): auth 62/0, chat 26/0, rating 2/0 (+1 skip:
  `TestRatedGamesSurviveAnOutboxOutage` только с `RATING_E2E_OUTAGE_GAMES`), security 17/0 (+2
  skip: наблюдение за ротацией JWT и продлением mTLS только с `*_WATCH`), foreign-ca — 4/4
  отказа (`tlsv1 alert unknown ca`) у auth, chat, game, rating.
- **С хоста**: `:28080` — `Register` 200 и `GetMe` вернул того же пользователя, RPC без токена —
  401; `:23003` — 200 (17 КБ); `:28443/api/version` — `v3.6.2`.
- **HPA** (`CHAOS=0 WAIT_SCALE_DOWN=1 make kind-chaos`, 2000 партий, 150 параллельно): 2 → 3
  через ~30 с при 73,5 активной партии на под, обратно 2 через ~30 с после конца нагрузки;
  67 985 ходов, 753 хода/с, p99 280 мс; сверка 2000 = 2000, потерянных ходов 0.
- **PDB**: одновременный `kubectl drain --pod-selector app.kubernetes.io/name=game` двух
  worker'ов с подами game: одна эвикция отклонена (`would violate the pod's disruption budget`)
  и прошла на ретрае, когда замена стала Ready (~4 с); Ready-подов game минимум 1, дрейны 9 и 4 с.
- **Chaos** (`make kind-chaos`, 14 шагов workflow, все под нагрузкой):
  - 2000 партий: `Accomplished`; `completed_by_client` 1990 = `completed_and_matching_db` 1990,
    `sessions_with_acknowledged_moves_missing` 0, `sessions_with_moves_missing_entirely` 0;
    10 партий оборвались у клиента (`socket closed: 4001`), 1974 передиала, p99 767 мс;
  - 4000 партий, как в фазе 3: `Accomplished`; 3999 = 3999, потерянных ходов 0, 1 таймаут
    клиента; 136 441 ход, 575 ходов/с, p99 754 мс; HPA 2 → 4 → 2; проектор догнал все 291 043
    записи журнала.
- **CDC при failover Postgres**: удалён primary `axon-pg-2` → primary `axon-pg-1` через
  3 мин 26 с (180 с — smart shutdown CNPG), коннектор всё время RUNNING, слот на новом primary
  `failover=true`, rating e2e после failover зелёный.
- **Preview**: метка `preview` на PR #21 → `axon-pr-21` (head `6e6b4945`) и 9 дочерних
  приложений Synced/Healthy через 6 мин 31 с, `Register` через gateway превью — 200; снятие
  метки → приложения и namespace `axon-pr-21` удалены через 6 мин 8 с.
- **С нуля**: `make kind-down && make kind-up` — exit 0 за 44 мин (2644 с): кластер и образы из
  кэша — 3 мин, Argo CD установлен в 00:49:52, все 19 приложений Synced/Healthy в 01:30:59.
  Почти всё время — холодная загрузка образов операторов и инфраструктуры из quay/ghcr/Docker Hub
  (operator Strimzi тянулся 10+ минут) на перегруженном хосте. Старый лимит ожидания 45 минут
  прошёл с запасом в минуту, поэтому `KIND_UP_TIMEOUT` по умолчанию поднят до 90 минут. С хоста
  на свежем кластере: `:28080` — 401 без токена, `:23003` — 200, `:28443` — `v3.6.2`.
- `make k8s-validate` — 161 манифест, 0 ошибок.
- После проверок кластер удалён `make kind-down` (хост перегревался); поднимается заново
  `make kind-up`. kind-e2e, HPA, PDB, chaos и preview гонялись на предыдущем кластере с теми же
  чартами, кроме лимита ожидания `kind-up`; на свежем кластере повторно не запускались.

Не проверено / ограничения:
- Повторный failover: бывший primary, вернувшись репликой, держит свою несинхронизированную
  копию `axon_outbox`, и PG17 не синхронизирует на него слот с тем же именем. Пока её не удалить
  на этой реплике (`pg_drop_replication_slot`), failover именно на неё потеряет позицию CDC;
  failover на вторую реплику покрыт.
- Удаление primary-пода ждёт smart shutdown 180 с, `smartShutdownTimeout` не трогали.
- Outage-тест rating и watch-тесты ротации/продления в kind не запускались; браузерный
  Playwright против kind не гонялся, Secure-cookie на `http://localhost` в Safari не проверена.
- После дрейна оба пода game оказались на одном узле: spread `ScheduleAnyway`, перераспределения
  нет.
- На перегруженном хосте control plane kind нестабилен: kube-controller-manager и scheduler
  7 раз теряли лидерство, `kubectl apply` workflow однажды шёл 4,5 минуты; `make kind-load`
  всех образов занял 25 минут.
