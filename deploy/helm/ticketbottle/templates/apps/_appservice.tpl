{{- /* Keeps serving while kube-proxy drops the pod from its Service.
       See docs/decisions/0015. */}}
{{- define "tb.preStopSleep" -}}
lifecycle:
  preStop:
    sleep: { seconds: {{ .Values.shutdown.preStopSleepSeconds }} }
{{- end -}}

{{- /* The digest of one app's own ConfigMap, so a change to it rolls that app alone. */}}
{{- define "tb.configDigest" -}}
{{- $want := printf "name: %s," .name -}}
{{- $doc := "" -}}
{{- range splitList "\n---" (include (print .ctx.Template.BasePath "/apps/config.yaml") .ctx) -}}
{{- if contains $want . }}{{ $doc = . }}{{ end -}}
{{- end -}}
{{- required (printf "no ConfigMap named %s in apps/config.yaml" .name) $doc | sha256sum -}}
{{- end -}}

{{- define "tb.appService" -}}
{{- $ := .ctx -}}
{{- if $.Values.apps.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .name }}
  namespace: {{ include "tb.namespace" $ }}
  labels: {{- include "tb.labels" $ | nindent 4 }}
spec:
  {{- $hpa := index ($.Values.autoscaling | default dict) .name | default dict }}
  {{- if not $hpa.enabled }}
  {{- /* When an HPA owns this Deployment, `replicas` must be omitted: leaving
         it in makes every `helm upgrade` reset the Deployment to the static
         count, and the HPA then has to scale back out from scratch. */}}
  replicas: {{ index $.Values.replicas .name | default 1 }}
  {{- end }}
  selector:
    matchLabels: { app: {{ .name }} }
  template:
    metadata:
      annotations:
        # Env is resolved at container creation, so a ConfigMap or Secret change
        # alone leaves pods running the old values. The digests roll them.
        checksum/config: {{ include "tb.configDigest" (dict "ctx" $ "name" .config) }}
        {{- if .secret }}
        checksum/secret: {{ include (print $.Template.BasePath "/apps/secrets.yaml") $ | sha256sum }}
        {{- end }}
      labels: { app: {{ .name }} }
    spec:
      {{- if $.Values.topologySpread.enabled }}
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels: { app: {{ .name }} }
      {{- end }}
      {{- if .serviceAccount }}
      serviceAccountName: {{ .serviceAccount }}
      {{- end }}
      {{- with .drainSeconds }}
      terminationGracePeriodSeconds: {{ add $.Values.shutdown.preStopSleepSeconds . 5 }}
      {{- end }}
      containers:
        - name: {{ .name }}
          image: {{ include "tb.image" (dict "ctx" $ "repo" .image) }}
          imagePullPolicy: {{ $.Values.image.pullPolicy }}
          {{- include "tb.resources" (dict "ctx" $ "name" .name) | nindent 10 }}
          envFrom:
            - configMapRef: { name: {{ .config }} }
            {{- if .secret }}
            - secretRef: { name: {{ .secret }} }
            {{- end }}
          {{- if gt (int .port) 0 }}
          ports: [{ containerPort: {{ .port }} }]
          {{- end }}
          {{- if .svcName }}
          {{- include "tb.preStopSleep" $ | nindent 10 }}
          {{- end }}
          {{- if eq .probe "tcp" }}
          readinessProbe:
            tcpSocket: { port: {{ .port }} }
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 12
          {{- else if eq .probe "http" }}
          readinessProbe:
            httpGet: { path: /api, port: {{ .port }} }
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 24
          {{- end }}
{{- if .svcName }}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ .svcName }}
  namespace: {{ include "tb.namespace" $ }}
  labels:
    app: {{ .name }}
    {{- include "tb.labels" $ | nindent 4 }}
spec:
  {{- if .nodePort }}
  type: NodePort
  {{- end }}
  selector: { app: {{ .name }} }
  ports:
    - name: {{ .portName | default "grpc" }}
      port: {{ .port }}
      targetPort: {{ .port }}
      {{- if .nodePort }}
      nodePort: {{ .nodePort }}
      {{- end }}
    {{- if and $.Values.monitoring.enabled .metricsPort }}
    - name: metrics
      port: {{ .metricsPort }}
      targetPort: {{ .metricsPort }}
    {{- end }}
{{- end }}
{{- end }}
{{- end -}}
