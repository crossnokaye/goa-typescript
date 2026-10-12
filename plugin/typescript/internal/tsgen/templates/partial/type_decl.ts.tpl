{{- jsdoc .Description "" -}}
export type {{ .Name }} = {{ .TSDef }};
{{- if .Recursive }}
export const {{ .Name }}: z.ZodType<{{ .Name }}> = z.lazy(() =>
  {{ .Zod }},
);{{ if .ZodNote }} // {{ .ZodNote }}{{ end }}
{{- else }}
export const {{ .Name }} = {{ .Zod }} satisfies z.ZodType<{{ .Name }}>;{{ if .ZodNote }} // {{ .ZodNote }}{{ end }}
{{- end }}
