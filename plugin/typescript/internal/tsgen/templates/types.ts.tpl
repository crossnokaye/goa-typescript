{{- if .ImportBlock }}

{{ .ImportBlock }}
{{- end }}
{{- range .Types }}

{{ template "partial_type_decl" . }}
{{- end }}
{{- if not .Types }}

export {};
{{- end }}
