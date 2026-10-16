{{- if .Description }}

{{ jsdoc .Description "" -}}
{{- end }}
{{- if .Endpoints }}
{{ range .Endpoints }}
export * from './{{ .FileName }}.js';
{{- if .Queries }}
export * from './{{ .FileName }}.query.js';
{{- end }}
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
