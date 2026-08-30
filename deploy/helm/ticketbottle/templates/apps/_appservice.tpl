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
  {{- /* When an HPA owns this Deployment, `replicas` MUST be omitted. Leaving it
         in means every `helm upgrade` resets the Deployment to the static count
         and the HPA has to scale back out from scratch — a slow, confusing fight
         between two controllers, and one of the most common Helm+HPA bugs. */}}
  replicas: {{ index $.Values.replicas .name | default 1 }}
  {{- end }}
  selector:
    matchLabels: { app: {{ .name }} }
  template:
    metadata:
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
      containers:
        - name: {{ .name }}
          image: {{ include "tb.image" (dict "ctx" $ "repo" .image) }}
          imagePullPolicy: {{ $.Values.image.pullPolicy }}
          {{- include "tb.resources" (dict "ctx" $ "name" .name) | nindent 10 }}
          envFrom:
            - configMapRef: { name: {{ .config }} }
          {{- if gt (int .port) 0 }}
          ports: [{ containerPort: {{ .port }} }]
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
  labels: {{- include "tb.labels" $ | nindent 4 }}
spec:
  {{- if .nodePort }}
  type: NodePort
  {{- end }}
  selector: { app: {{ .name }} }
  ports:
    - port: {{ .port }}
      targetPort: {{ .port }}
      {{- if .nodePort }}
      nodePort: {{ .nodePort }}
      {{- end }}
{{- end }}
{{- end }}
{{- end -}}
