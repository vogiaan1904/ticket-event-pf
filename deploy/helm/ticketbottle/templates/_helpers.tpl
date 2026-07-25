{{- define "tb.namespace" -}}
{{ .Values.namespace }}
{{- end -}}

{{- define "tb.labels" -}}
app.kubernetes.io/part-of: ticketbottle
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "tb.image" -}}
{{- $ := .ctx -}}
{{- printf "%s%s:%s" $.Values.image.registry .repo $.Values.image.tag -}}
{{- end -}}
