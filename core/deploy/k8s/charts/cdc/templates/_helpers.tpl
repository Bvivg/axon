{{- define "cdc.values" -}}
{{- $v := mergeOverwrite (deepCopy (omit .Values "cdc" "global")) (deepCopy (.Values.cdc | default dict)) -}}
{{- $g := mergeOverwrite (dict "postgres" (dict "host" "axon-pg-rw" "port" 5432 "database" "axon")) (deepCopy (.Values.global | default dict)) -}}
{{- $_ := set $v "global" $g -}}
{{- toYaml $v -}}
{{- end -}}

{{- define "cdc.labels" -}}
app.kubernetes.io/part-of: axon
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "cdc.jobSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end -}}
