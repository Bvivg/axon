{{- define "infra.globalDefaults" -}}
imageTag: local
imagePullPolicy: Never
kafka:
  replication: 1
  minInSync: 1
postgres:
  host: axon-pg-rw
  port: 5432
  database: axon
{{- end -}}

{{- define "infra.values" -}}
{{- $v := mergeOverwrite (deepCopy (omit .Values "infra" "global")) (deepCopy (.Values.infra | default dict)) -}}
{{- $g := mergeOverwrite (fromYaml (include "infra.globalDefaults" .)) (deepCopy (.Values.global | default dict)) -}}
{{- $_ := set $v "global" $g -}}
{{- toYaml $v -}}
{{- end -}}

{{- define "infra.labels" -}}
app.kubernetes.io/part-of: axon
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "infra.podSecurity" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
fsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "infra.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end -}}
