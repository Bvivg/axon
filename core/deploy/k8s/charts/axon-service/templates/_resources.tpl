{{- define "axon-service.service" -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ .Values.name }}
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  selector:
    {{- include "axon-service.selector" . | nindent 4 }}
  ports:
    {{- range $name, $port := .Values.ports }}
    - name: {{ $name }}
      port: {{ $port }}
      targetPort: {{ $name }}
      protocol: TCP
      {{- if and (eq $.Values.service.type "NodePort") (hasKey $.Values.service.nodePorts $name) }}
      nodePort: {{ index $.Values.service.nodePorts $name }}
      {{- end }}
    {{- end }}
{{- end -}}

{{- define "axon-service.pdb" -}}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ .Values.name }}
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
spec:
  minAvailable: {{ .Values.pdb.minAvailable }}
  unhealthyPodEvictionPolicy: AlwaysAllow
  selector:
    matchLabels:
      {{- include "axon-service.selector" . | nindent 6 }}
{{- end -}}

{{- define "axon-service.hpa" -}}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ .Values.name }}
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ .Values.name }}
  minReplicas: {{ .Values.hpa.minReplicas }}
  maxReplicas: {{ .Values.hpa.maxReplicas }}
  metrics:
    - type: Pods
      pods:
        metric:
          name: {{ .Values.hpa.metric }}
        target:
          type: AverageValue
          averageValue: {{ .Values.hpa.averageValue | quote }}
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
        - type: Pods
          value: 2
          periodSeconds: 15
    scaleDown:
      stabilizationWindowSeconds: {{ .Values.hpa.scaleDownWindowSeconds }}
      policies:
        - type: Pods
          value: 1
          periodSeconds: 30
{{- end -}}

{{- define "axon-service.servicemonitor" -}}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ .Values.name }}
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "axon-service.selector" . | nindent 6 }}
  endpoints:
    - port: {{ .Values.probes.port }}
      path: /metrics
      interval: 15s
{{- end -}}

{{- define "axon-service.certificate" -}}
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: {{ .Values.name }}-mtls
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
spec:
  secretName: {{ .Values.name }}-mtls
  commonName: {{ .Values.name }}
  dnsNames:
    - {{ .Values.name }}
    - {{ .Values.name }}.{{ .Release.Namespace }}.svc
    - {{ .Values.name }}.{{ .Release.Namespace }}.svc.cluster.local
  duration: {{ .Values.mtls.duration }}
  renewBefore: {{ .Values.mtls.renewBefore }}
  privateKey:
    algorithm: ECDSA
    size: 256
    rotationPolicy: Always
  usages:
    - digital signature
    - key encipherment
    - server auth
    - client auth
  issuerRef:
    group: cert-manager.io
    kind: ClusterIssuer
    name: {{ .Values.global.mtls.issuer }}
{{- end -}}

{{- define "axon-service.migrations" -}}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Values.name }}-migrations
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
  annotations:
    argocd.argoproj.io/hook: PreSync
    argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
    argocd.argoproj.io/sync-wave: "-1"
    helm.sh/hook: pre-install,pre-upgrade
    helm.sh/hook-weight: "-1"
data:
  {{- range $path, $_ := .Files.Glob "migrations/*.sql" }}
  {{ base $path }}: |-
    {{- $.Files.Get $path | nindent 4 }}
  {{- end }}
---
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ .Values.name }}-migrate
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
  annotations:
    argocd.argoproj.io/hook: PreSync
    argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
    helm.sh/hook: pre-install,pre-upgrade
spec:
  backoffLimit: 6
  activeDeadlineSeconds: 600
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Values.name }}-migrate
        app.kubernetes.io/part-of: axon
    spec:
      restartPolicy: OnFailure
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: migrate
          image: {{ .Values.migrations.image }}
          imagePullPolicy: IfNotPresent
          command: ["sh", "-ec"]
          args: ['exec migrate -path=/migrations -database "$POSTGRES_DSN" up']
          env:
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: pg-{{ .Values.database.role }}
                  key: password
            - name: POSTGRES_DSN
              value: {{ include "axon-service.dsn" (dict "root" .) | quote }}
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              memory: 128Mi
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          volumeMounts:
            - name: migrations
              mountPath: /migrations
              readOnly: true
      volumes:
        - name: migrations
          configMap:
            name: {{ .Values.name }}-migrations
{{- end -}}

{{- define "axon-service.all" -}}
{{- $_ := mergeOverwrite .Values (deepCopy (index .Values .Values.name | default dict)) -}}
{{- $_ := set .Values "global" (fromYaml (include "axon-service.global" .)) -}}
{{ include "axon-service.deployment" . }}
---
{{ include "axon-service.service" . }}
{{- if include "axon-service.replicated" . }}
---
{{ include "axon-service.pdb" . }}
{{- end }}
{{- if .Values.hpa.enabled }}
---
{{ include "axon-service.hpa" . }}
{{- end }}
{{- if .Values.serviceMonitor.enabled }}
---
{{ include "axon-service.servicemonitor" . }}
{{- end }}
{{- if and .Values.global.mtls.enabled .Values.mtls.enabled }}
---
{{ include "axon-service.certificate" . }}
{{- end }}
{{- if .Values.migrations.enabled }}
---
{{ include "axon-service.migrations" . }}
{{- end }}
{{- end -}}
