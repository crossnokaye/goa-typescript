{{- with .Request }}

{{ jsdoc (printf "%s is the request of %s." .TypeName $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .TypeName "TSDef" .TSDef "Zod" .Zod "ZodNote" .ZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- with .Result }}
{{- if .Inline }}

{{ jsdoc (printf "%s is the result of %s." .TypeName $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .TypeName "TSDef" .TSDef "Zod" .Zod "ZodNote" .ZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- end }}
{{- with .SSE }}
{{- if .EventInline }}

{{ jsdoc (printf "%s is an event streamed by %s." .EventTypeName $.StreamFuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .EventTypeName "TSDef" .EventTSDef "Zod" .EventZod "ZodNote" .EventZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- end }}
{{- with .WS }}
{{- if .RecvInline }}

{{ jsdoc (printf "%s is a message the server sends on %s." .RecvTypeName $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .RecvTypeName "TSDef" .RecvTSDef "Zod" .RecvZod "ZodNote" .RecvZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- if .SendInline }}

{{ jsdoc (printf "%s is a message the client sends on %s." .SendTypeName $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .SendTypeName "TSDef" .SendTSDef "Zod" .SendZod "ZodNote" .SendZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- end }}
{{- range .Errors }}
{{- if .Inline }}

{{ jsdoc (printf "%s is the detail of the %q error of %s." .TypeName .Name $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .TypeName "TSDef" .TSDef "Zod" .Zod "ZodNote" .ZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- end }}
