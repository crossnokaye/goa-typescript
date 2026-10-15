{{ jsdoc .Doc "" -}}
export async function {{ .FuncName }}(
  client: Client,
{{- if .Request }}
  payload{{ if .Request.Optional }}?{{ end }}: {{ .Request.TypeName }},
{{- else }}
  payload?: undefined,
{{- end }}
  options?: WebSocketOptions,
): Promise<CallResult<{{ .ResultTS }}, {{ .ErrorTypeName }}>> {
{{- if .Request }}
  const wire = encode({{ .Request.TypeName }}, payload{{ if .Request.Optional }} ?? {}{{ end }});
  if (!wire.ok) {
    return wire;
  }
  return connectWebSocket(client, {{ .SpecName }}, wire.value, options, {
{{- else }}
  return connectWebSocket(client, {{ .SpecName }}, payload, options, {
{{- end }}
    recv: {{ if .WS.RecvTypeName }}{{ .WS.RecvTypeName }}{{ else }}null{{ end }},
    send: {{ if .WS.SendTypeName }}{{ .WS.SendTypeName }}{{ else }}null{{ end }},
    single: {{ .WS.Single }},
  });
}
