{{- if .Description }}

{{ jsdoc .Description "" -}}
{{- end }}
{{- if .Endpoints }}
{{ range .Endpoints }}
export * from './{{ .FileName }}.js';
{{- end }}
{{- else }}

export {};
{{- end }}
{{- if .Skipped }}

// No function is generated for these methods:
{{- range .Skipped }}
//   {{ .Method }}: {{ .Reason }}
{{- end }}
{{- end }}
{{- if .QueryNone }}

// No query options are generated for these methods:
{{- range .QueryNone }}
//   {{ .Method }}: {{ .Reason }}
{{- end }}
{{- end }}
