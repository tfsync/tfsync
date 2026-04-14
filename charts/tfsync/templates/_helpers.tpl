{{- define "tfsync.labels" -}}
app.kubernetes.io/name: tfsync
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
