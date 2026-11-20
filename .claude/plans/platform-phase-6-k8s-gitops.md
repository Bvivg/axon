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
    `core/deploy/postgres/init/*` (symlink в `files/`). `axon-pg-rw` — запись, `axon-pg-ro` —
    `GAME_READ_DATABASE_URL`.
  - Strimzi: `KafkaNodePool` ×3 KRaft (dual-role, rack по зоне), RF=3, `min.insync.replicas=2`;
    `KafkaTopic` для журнала и лизов game (24 партиции) и для топиков группы `forever`, которые
    рендерятся из `topic.creation.*` в `outbox-connector.json` (сейчас `auth.audit.v1`,
    `game.finished.v1`, `rating.changed.v1`, `retention.ms=-1`); `KafkaConnect` на образе
    `Dockerfile.connect` (Strimzi Kafka + плагин Postgres из `debezium/connect:3.7.0.Final`).
    Остальные outbox-топики создаёт Connect по тем же правилам, что в compose.
  - Redis Operator: `RedisReplication` `app-redis` ×3 со встроенным sentinel ×3 (в 0.27
    sentinel — поле `RedisReplication`, отдельный `RedisSentinel` не нужен) и одиночный `Redis`
    `edge-redis` (64 МБ, `volatile-ttl`).
  - MinIO — Deployment + PVC + Sync-хук создания бакетов; Apicurio Registry — Deployment.
- **CDC** — чарт `charts/cdc`, волна после сервисов, потому что публикация требует outbox-таблиц
  из миграций: Job `cdc-setup` (тот же `core/deploy/cdc/setup.sh`), `KafkaConnector`
  `axon-outbox`, чей `config` целиком рендерится из `core/deploy/cdc/outbox-connector.json`
  (`connector.class`/`tasks.max` уходят в `spec`), и Job регистрации `events.proto` в Apicurio.
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
    prometheus-adapter), min 2 / max 6, `averageValue` 50, scale-down окно 60 с.
  - PDB `minAvailable: 1` у всех сервисов с ≥2 репликами (auth, chat, gateway, game, rating).
  - mTLS: `platform` создаёт self-signed CA и `ClusterIssuer` `axon-internal`; `Certificate` на
    сервис (ECDSA, 1 ч, `renewBefore` 20 мин, SAN `<svc>`, `<svc>.<ns>.svc`), секрет монтируется в
    `/etc/axon/tls`; списки клиентов как в `docker-compose.mtls.yml`.
  - gateway в kind: `TRUSTED_PROXIES=1`, лимиты 600/60 в минуту и origin `localhost:3000` в CORS —
    как в `docker-compose.e2e.yml`, иначе e2e auth/chat не различают вызывающих.
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
  сервера). E2E идут изнутри кластера: rating e2e пишет прямо в Kafka и Postgres, mTLS e2e
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
- HPA: `CHAOS=0 make kind-chaos` — реплики game растут с 2 до ≥3, после нагрузки возвращаются к 2;
  сверка 0 потерянных подтверждённых ходов.
- PDB: одновременный `kubectl drain --pod-selector app.kubernetes.io/name=game` двух worker'ов с
  подами game — вторая эвикция ждёт PDB, Ready-подов game всегда ≥1.
- Chaos: `make kind-chaos` — workflow `Accomplished`, сверка: `completed_and_matching_db ==
  completed_by_client`, `sessions_with_acknowledged_moves_missing = 0`.
- Preview: метка `preview` на открытом PR → `axon-pr-<n>` Synced/Healthy; снятие метки →
  namespace удалён.
- `make k8s-validate` — 0 ошибок; `make kind-down` удаляет кластер.

## Критерий готовности
`make kind-up` поднимает весь стек, Argo CD зелёный, chaos-сценарии фазы 3 проходят в кластере.
