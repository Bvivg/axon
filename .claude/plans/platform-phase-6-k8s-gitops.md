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
  `kind load docker-image`, тег `local`, `imagePullPolicy: Never`; отдельный registry-контейнер не
  нужен. Наружу через `extraPortMappings` на NodePort: gateway, web, Argo CD — на порты, не
  пересекающиеся со стеками `axon` и e2e (например 28080, 23003, 28443).
- **GitOps** — `make kind-up` ставит только Argo CD (Helm, версия зафиксирована) и корневой
  Application `core/deploy/k8s/argocd/root.yaml` (app-of-apps). Остальное ставит Argo CD из
  репозитория `Bvivg/axon` (публичный), ревизия — текущая ветка (`ARGO_REVISION`, по умолчанию
  `git rev-parse --abbrev-ref HEAD`, ветка должна быть запушена). Порядок — sync waves:
  операторы и CRD → секреты → инфраструктура → миграции → сервисы.
- **Операторы** (upstream Helm-чарты с фиксированными версиями): cert-manager, CloudNativePG,
  Strimzi, Redis Operator (OT-Container-Kit), kube-prometheus-stack в урезанном виде (Prometheus
  Operator + Prometheus, без Alertmanager/Grafana), prometheus-adapter, Chaos Mesh (runtime
  containerd, сокет kind).
- **Инфраструктура** — чарт `charts/infra`:
  - CNPG `Cluster` `axon-pg`: 3 инстанса, `wal_level=logical`, managed roles на сервис и
    `debezium` (replication), пароли из секретов; схемы — Job по `core/deploy/postgres/init`.
  - Strimzi: `KafkaNodePool` ×3 KRaft (dual-role), RF=3, `min.insync.replicas=2`; `KafkaTopic` для
    журнала, лизов, outbox-топиков; `KafkaConnect` с образом Strimzi Kafka + плагин Debezium
    (собирается локально, `kind load`), `KafkaConnector` — тот же Outbox Event Router, что
    `core/deploy/cdc/outbox-connector.json`; Apicurio Registry — Deployment.
  - Redis Operator: `RedisReplication` + `RedisSentinel` для app-redis и одиночный для edge-redis.
  - MinIO — Deployment + Job создания бакета аватаров.
- **Секреты** — PreSync Job `secrets-bootstrap` (образ с kubectl и openssl, свой ServiceAccount и
  Role на `secrets` в namespace) создаёт недостающие секреты: пароли ролей Postgres, Redis, MinIO,
  стартовый ключ JWT, `JWT_KEY_ENCRYPTION_KEY`. Существующие не трогает, поэтому повторный sync
  не меняет ключи. В git секретов нет. Так же работают preview-namespace.
- **Сервисы** — library chart `charts/axon-service` (Deployment, Service, PDB, HPA,
  ServiceMonitor, Certificate, probes на admin-порт, `topologySpreadConstraints` по zone и
  hostname, `securityContext` non-root/read-only) и тонкий чарт на сервис: `auth`, `chat`, `game`,
  `gateway`, `rating`, `web`. Миграции — Job с хуком `PreSync` в чарте сервиса.
  - game: `GAME_ADVERTISE_ADDR` из `status.podIP` (downward API), `GAME_INSTANCE_ID` — имя пода;
    Deployment, не StatefulSet: владение партиями решают лизы.
  - HPA game по `game_active_sessions` (pods-метрика через prometheus-adapter), min 2 / max 6,
    `averageValue` подбирается замером; короткое окно стабилизации для демонстрации.
  - PDB `minAvailable: 1` у всех сервисов с ≥2 репликами (auth, chat, gateway, game, rating).
  - mTLS: cert-manager `ClusterIssuer` с собственным CA, `Certificate` на сервис (1 ч,
    `renewBefore` 20 мин), секрет монтируется в путь `MTLS_*`; списки допустимых клиентов те же,
    что в `docker-compose.mtls.yml`. Перечитывание по mtime работает и с обновлением секрета kubelet.
  - Calls/LiveKit/coturn в кластер не переносятся (`CALLS_ENABLED=false`).
- **Preview на PR** — ApplicationSet с генератором Pull Request (GitHub, `Bvivg/axon`, фильтр по
  метке `preview`, `requeueAfterSeconds: 300`): namespace `axon-pr-<номер>`, ревизия — head SHA PR,
  values `values-preview.yaml` (по одной реплике, Postgres 1 инстанс, Kafka 1 брокер, HPA/PDB
  выключены). Снятие метки или закрытие PR удаляет окружение.
- **Chaos Mesh** — `core/deploy/k8s/chaos/`: `Workflow` с теми же шагами, что
  `core/deploy/chaos/scenario.sh` (задержка 300 мс до primary Postgres 20 с, обрыв Redis 10 с,
  pod-kill game/chat/gateway/auth по очереди) плюс kill брокера Kafka. Нагрузка — `cmd/loadgen`
  game Job'ом в кластере, сверка — та же, что в фазе 3 (ход, подтверждённый клиенту, есть в БД).
- **CI** — job `k8s`: `helm lint` всех чартов и `kubeconform` по отрендеренным манифестам
  (`values-kind.yaml` и `values-preview.yaml`), со строгой проверкой CRD через каталог схем.
- **Make** — `kind-up` (кластер, образы, Argo CD, root app, ожидание Healthy), `kind-down`
  (`kind delete cluster --name axon`), `kind-load` (пересборка и загрузка образов),
  `kind-chaos` (нагрузка + workflow + сверка).

## Файлы
- `core/deploy/k8s/kind/cluster.yaml`
- `core/deploy/k8s/argocd/` — values Argo CD, `root.yaml`, `apps/*.yaml`, `preview-appset.yaml`
- `core/deploy/k8s/charts/axon-service/`, `charts/{auth,chat,game,gateway,rating,web}/`,
  `charts/infra/`, `charts/secrets-bootstrap/`
- `core/deploy/k8s/values-kind.yaml`, `core/deploy/k8s/values-preview.yaml`
- `core/deploy/k8s/chaos/`, `core/deploy/k8s/Dockerfile.connect`
- `Makefile`, `.github/workflows/ci.yml`, `.claude/rules/infra.md` (раздел K8s), `roadmap.md`

## Как проверяется
- `make kind-up` на удалённом кластере → exit 0; `kubectl -n argocd get applications` — все
  `Synced`/`Healthy`.
- E2E auth, game (полная партия каждой игры) и rating — существующие Go-сьюты против gateway
  кластера (`GATEWAY_URL=http://localhost:28080`) — зелёные.
- mTLS: под без сертификата и с сертификатом чужого CA получает отказ от auth/chat/game/rating.
- HPA: под нагрузкой `cmd/loadgen` реплики game растут с 2 до ≥3, после — возвращаются к 2;
  во время смены реплик 0 потерянных подтверждённых ходов.
- PDB: `kubectl drain` worker'а с подами game — эвикция ждёт, у game всегда ≥1 Ready.
- Chaos: `make kind-chaos` — workflow `Succeeded`, сверка: `completed_and_matching_db ==
  completed_by_client`, `sessions_with_acknowledged_moves_missing = 0`.
- Preview: метка `preview` на открытом PR → Application `axon-pr-<n>` Synced/Healthy; снятие метки →
  namespace удалён.
- `helm lint`, `kubeconform` — 0 ошибок; `make kind-down` удаляет кластер.

## Критерий готовности
`make kind-up` поднимает весь стек, Argo CD зелёный, chaos-сценарии фазы 3 проходят в кластере.
