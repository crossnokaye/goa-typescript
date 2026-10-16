{{ .ImportBlock }}
{{- template "partial_endpoint_types" . }}

{{ if .ExportSpec }}{{ jsdoc (printf "%s is the transport mapping of %s, for the sibling query file; it is not a\npublic contract.\n\n@internal" .SpecName .FuncName) "" }}export {{ end }}const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};

{{ jsdoc (printf "%s is everything %s can fail with.\n\nA connection the server refuses, even with an error the design declares, is a\n`NetworkError`: the browser hides the handshake response." .ErrorTypeName .FuncName) "" -}}
export type {{ .ErrorTypeName }} ={{ .ErrorTS }};

{{ template "partial_websocket_func" . }}
