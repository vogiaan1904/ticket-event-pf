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
  replicas: 1
  selector:
    matchLabels: { app: {{ .name }} }
  template:
    metadata:
      labels: { app: {{ .name }} }
    spec:
      containers:
        - name: {{ .name }}
          image: {{ .image }}
          imagePullPolicy: IfNotPresent
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
