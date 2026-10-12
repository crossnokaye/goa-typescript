{{ jsdoc (printf "%s lists the errors the design declares for %s, by HTTP status." .ErrorsName .FuncName) "" -}}
export const {{ .ErrorsName }} = {{ .ErrorTable }};

{{ jsdoc (printf "%s is everything %s can fail with." .ErrorTypeName .FuncName) "" -}}
export type {{ .ErrorTypeName }} ={{ .ErrorTS }};
