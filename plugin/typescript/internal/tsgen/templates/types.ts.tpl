{{- if .ImportBlock }}

{{ .ImportBlock }}
{{- end }}
{{- range .Types }}

{{ template "partial_type_decl" . }}
{{- with .Request }}

{{ template "partial_type_decl" . }}
{{- end }}
{{- end }}
{{- if not .Types }}

export {};
{{- end }}
