{{ .ImportBlock }}
{{- template "partial_endpoint_types" . }}

const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};
{{- with .Stream }}

const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};
{{- end }}

const {{ .ResponsesName }} = {{ .ResponsesTS }};

{{ template "partial_endpoint_errors" . }}

{{ template "partial_rest_func" . }}
{{- with .Stream }}

{{ template "partial_sse_func" (dict "Endpoint" $ "FuncName" .FuncName "SpecName" .SpecName "Doc" $.StreamDoc) }}
{{- end }}
{{- range .Queries }}
{{- range .Decls }}

{{ jsdoc .Doc "" -}}
{{ .Code }}
{{- end }}
{{- end }}
