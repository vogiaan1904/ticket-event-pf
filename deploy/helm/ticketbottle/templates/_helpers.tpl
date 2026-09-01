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

{{/*
Render a `resources:` block for a workload, or nothing when the values file has
no entry for it. Call as:
  {{ include "tb.resources" (dict "ctx" $ "name" "app-gateway") }}
*/}}
{{- define "tb.resources" -}}
{{- $res := index (.ctx.Values.resources | default dict) .name | default dict -}}
{{- with $res }}
resources:
{{ toYaml . | indent 2 }}
{{- end -}}
{{- end -}}
