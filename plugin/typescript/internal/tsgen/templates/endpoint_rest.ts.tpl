{{ .ImportBlock }}
{{- template "partial_endpoint_types" . }}

{{ if .ExportSpec }}{{ jsdoc (printf "%s is the transport mapping of %s, for the sibling query file; it is not a\npublic contract.\n\n@internal" .SpecName .FuncName) "" }}export {{ end }}const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};
{{- with .Stream }}

{{ if $.ExportSpec }}{{ jsdoc (printf "%s is the transport mapping of %s, for the sibling query file; it is not a\npublic contract.\n\n@internal" .SpecName .FuncName) "" }}export {{ end }}const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};
{{- end }}

const {{ .ResponsesName }} = {{ .ResponsesTS }};

{{ template "partial_endpoint_errors" . }}

{{ template "partial_rest_func" . }}
{{- with .Stream }}

{{ template "partial_sse_func" (dict "Endpoint" $ "FuncName" .FuncName "SpecName" .SpecName "Doc" $.StreamDoc) }}
{{- end }}
