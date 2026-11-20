{{- define "axon-service.globalDefaults" -}}
imageTag: local
imagePullPolicy: Never
appEnv: development
logLevel: info
logFormat: json
mtls:
  enabled: true
  issuer: axon-internal
  reload: 30s
postgres:
  host: axon-pg-rw
  readHost: axon-pg-ro
  port: 5432
  database: axon
  sslmode: require
kafka:
  brokers: axon-kafka-kafka-bootstrap:9092
redis:
  app: app-redis-master:6379
  edge: edge-redis:6379
publicGatewayURL: http://localhost:28080
publicWebURL: http://localhost:23003
{{- end -}}

{{- define "axon-service.global" -}}
{{- $defaults := fromYaml (include "axon-service.globalDefaults" .) -}}
{{- toYaml (mergeOverwrite $defaults (deepCopy (.Values.global | default dict))) -}}
{{- end -}}

{{- define "axon-service.selector" -}}
app.kubernetes.io/name: {{ .Values.name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "axon-service.labels" -}}
{{ include "axon-service.selector" . }}
app.kubernetes.io/part-of: axon
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "axon-service.replicated" -}}
{{- if or .Values.hpa.enabled (ge (int .Values.replicas) 2) -}}true{{- end -}}
{{- end -}}

{{- define "axon-service.dsn" -}}
{{- $g := fromYaml (include "axon-service.global" .root) -}}
postgres://{{ .root.Values.database.role }}_service:$(DB_PASSWORD)@{{ .host | default $g.postgres.host }}:{{ $g.postgres.port }}/{{ $g.postgres.database }}?sslmode={{ $g.postgres.sslmode }}&search_path={{ .root.Values.database.schema | default .root.Values.database.role }}
{{- end -}}
