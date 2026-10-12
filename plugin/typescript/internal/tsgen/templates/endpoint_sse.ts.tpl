
{{ .ImportBlock }}
{{- with .Request }}

{{ jsdoc (printf "%s is the request of %s." .TypeName $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .TypeName "TSDef" .TSDef "Zod" .Zod "ZodNote" .ZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- with .SSE }}
{{- if .EventInline }}

{{ jsdoc (printf "%s is an event streamed by %s." .EventTypeName $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .EventTypeName "TSDef" .EventTSDef "Zod" .EventZod "ZodNote" .EventZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- end }}

const {{ .SpecName }}: EndpointSpec = {{ .SpecTS }};

{{ jsdoc (printf "%s lists the errors the design declares for %s, by HTTP status." .ErrorsName .FuncName) "" -}}
export const {{ .ErrorsName }} = {{ .ErrorTable }};

{{ jsdoc (printf "%s is everything %s can fail with." .ErrorTypeName .FuncName) "" -}}
export type {{ .ErrorTypeName }} ={{ .ErrorTS }};

{{ jsdoc .Doc "" -}}
export async function {{ .FuncName }}(
  client: Client,
{{- if .Request }}
  payload: {{ .Request.TypeName }},
{{- end }}
  options?: StreamOptions,
): Promise<Result<{{ .ResultTS }}, {{ .ErrorTypeName }}>> {
{{- if .Request }}
  const wire = encode({{ .Request.TypeName }}, payload);
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
    return err(await goaErrorDecoder({{ .ErrorsName }})(res));
  }
  if (!/^text\/event-stream/i.test(res.headers.get("content-type") ?? "") || !res.body) {
    return err(new UnexpectedResponseError(res, await res.text()));
  }
  return ok(
    new EventStream(
      res,
      sseDecoder({{ .SSE.EventTypeName }}, {
        encoding: {{ tsString .SSE.DataEncoding }},
        data: {{ nullable .SSE.DataField }},
        id: {{ nullable .SSE.IDField }},
        event: {{ nullable .SSE.EventField }},
        retry: {{ nullable .SSE.RetryField }},
      }),
    ),
  );
}
