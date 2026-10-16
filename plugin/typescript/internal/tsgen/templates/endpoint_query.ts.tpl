{{ .ImportBlock }}
{{- range .Queries }}
{{- range .Decls }}

{{ jsdoc .Doc "" -}}
{{ .Code }}
{{- end }}
{{- end }}
