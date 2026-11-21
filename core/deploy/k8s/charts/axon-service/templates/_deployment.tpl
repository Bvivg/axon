{{- define "axon-service.env" -}}
{{- $g := fromYaml (include "axon-service.global" .) -}}
- name: APP_ENV
  value: {{ $g.appEnv | quote }}
- name: LOG_LEVEL
  value: {{ $g.logLevel | quote }}
- name: LOG_FORMAT
  value: {{ $g.logFormat | quote }}
{{- range .Values.fieldEnv }}
- name: {{ .name }}
  valueFrom:
    fieldRef:
      fieldPath: {{ .fieldPath }}
{{- end }}
{{- range $name, $ref := .Values.secretEnv }}
- name: {{ $name }}
  valueFrom:
    secretKeyRef:
      name: {{ $ref.secret }}
      key: {{ $ref.key }}
{{- end }}
{{- if .Values.database.role }}
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: pg-{{ .Values.database.role }}
      key: password
- name: POSTGRES_DSN
  value: {{ include "axon-service.dsn" (dict "root" .) | quote }}
{{- end }}
{{- if and $g.mtls.enabled .Values.mtls.enabled }}
- name: MTLS_CERT_FILE
  value: /etc/axon/tls/tls.crt
- name: MTLS_KEY_FILE
  value: /etc/axon/tls/tls.key
- name: MTLS_CA_FILE
  value: /etc/axon/tls/ca.crt
- name: MTLS_RELOAD_INTERVAL
  value: {{ $g.mtls.reload | quote }}
{{- if .Values.mtls.allowedClients }}
- name: MTLS_ALLOWED_CLIENTS
  value: {{ .Values.mtls.allowedClients | quote }}
{{- end }}
{{- range $name, $value := .Values.mtls.env }}
- name: {{ $name }}
  value: {{ tpl (toString $value) $ | quote }}
{{- end }}
{{- else }}
{{- range $name, $value := .Values.mtls.plainEnv }}
- name: {{ $name }}
  value: {{ tpl (toString $value) $ | quote }}
{{- end }}
{{- end }}
{{- range $name, $value := .Values.env }}
- name: {{ $name }}
  value: {{ tpl (toString $value) $ | quote }}
{{- end }}
{{- end -}}

{{- define "axon-service.deployment" -}}
{{- $g := fromYaml (include "axon-service.global" .) -}}
{{- $mtls := and $g.mtls.enabled .Values.mtls.enabled -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Values.name }}
  labels:
    {{- include "axon-service.labels" . | nindent 4 }}
spec:
  {{- if not .Values.hpa.enabled }}
  replicas: {{ .Values.replicas }}
  {{- end }}
  revisionHistoryLimit: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
  selector:
    matchLabels:
      {{- include "axon-service.selector" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "axon-service.labels" . | nindent 8 }}
    spec:
      automountServiceAccountToken: false
      enableServiceLinks: false
      terminationGracePeriodSeconds: {{ .Values.terminationGracePeriodSeconds }}
      securityContext:
        runAsNonRoot: true
        runAsUser: {{ .Values.securityContext.runAsUser }}
        runAsGroup: {{ .Values.securityContext.runAsUser }}
        fsGroup: {{ .Values.securityContext.runAsUser }}
        seccompProfile:
          type: RuntimeDefault
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              {{- include "axon-service.selector" . | nindent 14 }}
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              {{- include "axon-service.selector" . | nindent 14 }}
      containers:
        - name: {{ .Values.name }}
          image: "{{ .Values.image }}:{{ $g.imageTag }}"
          imagePullPolicy: {{ $g.imagePullPolicy }}
          {{- with .Values.args }}
          args:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          ports:
            {{- range $name, $port := .Values.ports }}
            - name: {{ $name }}
              containerPort: {{ $port }}
              protocol: TCP
            {{- end }}
          env:
            {{- include "axon-service.env" . | nindent 12 }}
          startupProbe:
            httpGet:
              path: {{ .Values.probes.livePath }}
              port: {{ .Values.probes.port }}
            periodSeconds: 3
            timeoutSeconds: 3
            failureThreshold: 60
          livenessProbe:
            httpGet:
              path: {{ .Values.probes.livePath }}
              port: {{ .Values.probes.port }}
            periodSeconds: 10
            timeoutSeconds: 3
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: {{ .Values.probes.readyPath }}
              port: {{ .Values.probes.port }}
            periodSeconds: 3
            timeoutSeconds: 3
            failureThreshold: 2
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          volumeMounts:
            {{- range $i, $path := .Values.writablePaths }}
            - name: scratch-{{ $i }}
              mountPath: {{ $path }}
            {{- end }}
            {{- if $mtls }}
            - name: mtls
              mountPath: /etc/axon/tls
              readOnly: true
            {{- end }}
      volumes:
        {{- range $i, $path := .Values.writablePaths }}
        - name: scratch-{{ $i }}
          emptyDir:
            sizeLimit: 256Mi
        {{- end }}
        {{- if $mtls }}
        - name: mtls
          secret:
            secretName: {{ .Values.name }}-mtls
            defaultMode: 0440
        {{- end }}
{{- end -}}
