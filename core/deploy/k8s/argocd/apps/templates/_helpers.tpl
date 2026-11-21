{{- define "apps.syncPolicy" -}}
automated:
  prune: true
  selfHeal: true
syncOptions:
  - CreateNamespace=true
  - ServerSideApply=true
retry:
  limit: 30
  backoff:
    duration: 10s
    factor: 2
    maxDuration: 2m
{{- end -}}

{{- define "apps.application" -}}
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .name }}
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: axon-platform
  annotations:
    argocd.argoproj.io/sync-wave: {{ .wave | quote }}
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: {{ .chart.repo }}
    chart: {{ .chart.chart }}
    targetRevision: {{ .chart.version }}
    helm:
      releaseName: {{ .name }}
      valuesObject:
        {{- toYaml .values | nindent 8 }}
  destination:
    server: https://kubernetes.default.svc
    namespace: {{ .namespace }}
  syncPolicy:
    {{- include "apps.syncPolicy" . | nindent 4 }}
{{- end -}}
