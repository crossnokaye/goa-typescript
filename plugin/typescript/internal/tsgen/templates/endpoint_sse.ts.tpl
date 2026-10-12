{{ .ImportBlock }}
{{- template "partial_endpoint_types" . }}

const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};

{{ template "partial_endpoint_errors" . }}

{{ template "partial_sse_func" (dict "Endpoint" . "FuncName" .FuncName "SpecName" .SpecName "Doc" .Doc) }}
