{{ jsdoc .Doc "" -}}
export async function {{ .FuncName }}(
  client: Client,
{{- if .Request }}
  payload: {{ .Request.TypeName }},
{{- end }}
{{- if .RawBody }}
  body: BodyInit,
{{- end }}
  options?: RequestOptions,
): Promise<CallResult<{{ .ResultTS }}, {{ .ErrorTypeName }}>> {
{{- if .Request }}
  const wire = encode({{ .Request.TypeName }}, payload);
  if (!wire.ok) {
    return wire;
  }
  const sent = await send(client, {{ .SpecName }}, wire.value, options{{ if .RawBody }}, body{{ end }});
{{- else }}
  const sent = await send(client, {{ .SpecName }}, undefined, options{{ if .RawBody }}, body{{ end }});
{{- end }}
  if (!sent.ok) {
    return sent;
  }
  return decodeResponse(sent.value, {{ if .Result.TypeName }}{{ .Result.TypeName }}{{ else }}null{{ end }}, {{ .ResponsesName }}, goaErrorDecoder({{ .ErrorsName }}));
}
