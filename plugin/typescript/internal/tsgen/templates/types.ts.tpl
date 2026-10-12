{{- if .ImportBlock }}

{{ .ImportBlock }}
{{- end }}
{{- range .Types }}

{{ template "partial_type_decl" . }}
{{- with .Input }}

{{ template "partial_type_decl" . }}
{{- end }}
{{- end }}
{{- if not .Types }}

export {};
{{- end }}
