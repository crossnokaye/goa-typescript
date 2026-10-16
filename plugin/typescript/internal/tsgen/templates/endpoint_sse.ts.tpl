{{ .ImportBlock }}
{{- template "partial_endpoint_types" . }}

{{ if .ExportSpec }}{{ jsdoc (printf "%s is the transport mapping of %s." .SpecName .FuncName) "" }}export {{ end }}const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};

{{ template "partial_endpoint_errors" . }}

{{ template "partial_sse_func" (dict "Endpoint" . "FuncName" .FuncName "SpecName" .SpecName "Doc" .Doc) }}
