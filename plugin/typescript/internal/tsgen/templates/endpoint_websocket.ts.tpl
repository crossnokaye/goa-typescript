{{ .ImportBlock }}
{{- template "partial_endpoint_types" . }}

const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};

{{ jsdoc (printf "%s is everything %s can fail with.\n\nA connection the server refuses, even with an error the design declares, is a\n`NetworkError`: the browser hides the handshake response." .ErrorTypeName .FuncName) "" -}}
export type {{ .ErrorTypeName }} ={{ .ErrorTS }};

{{ template "partial_websocket_func" . }}
{{- range .Queries }}
{{- range .Decls }}

{{ jsdoc .Doc "" -}}
{{ .Code }}
{{- end }}
{{- end }}
