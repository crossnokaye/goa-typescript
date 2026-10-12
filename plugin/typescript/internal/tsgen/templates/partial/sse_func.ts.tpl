{{ jsdoc .Doc "" -}}
export async function {{ .FuncName }}(
  client: Client,
{{- if .Endpoint.Request }}
  payload: {{ .Endpoint.Request.TypeName }},
{{- end }}
  options?: StreamOptions,
): Promise<CallResult<EventStream<{{ .Endpoint.SSE.EventTypeName }}>, {{ .Endpoint.ErrorTypeName }}>> {
{{- if .Endpoint.Request }}
  const wire = encode({{ .Endpoint.Request.TypeName }}, payload);
  if (!wire.ok) {
    return wire;
  }
  const sent = await send(client, {{ .SpecName }}, wire.value, options);
{{- else }}
  const sent = await send(client, {{ .SpecName }}, undefined, options);
{{- end }}
  if (!sent.ok) {
    return sent;
  }
  const res = sent.value;
  if (res.status !== 200) {
    return { ok: false, error: await goaErrorDecoder({{ .Endpoint.ErrorsName }})(res), response: res };
  }
  if (!/^text\/event-stream/i.test(res.headers.get("content-type") ?? "") || !res.body) {
    return { ok: false, error: new UnexpectedResponseError(res, await res.text()), response: res };
  }
  return {
    ok: true,
    value: new EventStream(
      res,
      sseDecoder({{ .Endpoint.SSE.EventTypeName }}, {
        encoding: {{ tsString .Endpoint.SSE.DataEncoding }},
        data: {{ nullable .Endpoint.SSE.DataField }},
        id: {{ nullable .Endpoint.SSE.IDField }},
        event: {{ nullable .Endpoint.SSE.EventField }},
        retry: {{ nullable .Endpoint.SSE.RetryField }},
      }),
    ),
    response: res,
  };
}
