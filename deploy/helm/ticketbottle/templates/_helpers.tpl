{{- define "tb.namespace" -}}
{{ .Values.namespace }}
{{- end -}}

{{- define "tb.labels" -}}
app.kubernetes.io/part-of: ticketbottle
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
