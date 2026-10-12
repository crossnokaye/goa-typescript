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
{{- range .Errors }}
{{- if .Inline }}

{{ jsdoc (printf "%s is the detail of the %q error of %s." .TypeName .Name $.FuncName) "" -}}
{{ template "partial_type_decl" (dict "Name" .TypeName "TSDef" .TSDef "Zod" .Zod "ZodNote" .ZodNote "Recursive" false "Description" "") }}
{{- end }}
{{- end }}
