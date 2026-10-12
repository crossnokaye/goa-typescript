### Testing Your Changes

To see sample generation results, gen the toy example. It is a nested Go module, so run from its directory:

```bash
cd examples/toy
go run goa.design/goa/v3/cmd/goa@v3.34.0 gen github.com/xeger/goa-typescript/examples/toy/design -o .
pnpm install --no-frozen-lockfile
```

The result appears under `examples/toy/gen` which is gitignored; the TypeScript client is `examples/toy/gen/ts`.

Tests:

```bash
pnpm install && pnpm -r type-check && pnpm -r test   # the TypeScript runtime (runtime/)
go vet ./... && go test ./...                        # generator tests and the toy pipeline
go test ./plugin/typescript/internal/tsgen -update   # rewrite the golden files after an intentional change
```

- `go test ./...` runs the toy pipeline (gen -> pnpm install -> tsc -> Go server -> vitest), which needs Node 24 and pnpm, and compiles every golden fixture with `tsc`, which needs `pnpm install` at the repository root. Both skip when their tools are missing, except in CI. `go test -short ./...` skips them always.
- `runtime/src/*.ts` is embedded into the Go module and copied verbatim into generated clients. Those files may import only each other (with a `.js` suffix) and `zod`.
- The plugin reads `goa.design/goa/v3/expr` only and must never mutate the design: goa fails `goa gen` if a plugin does. `TestBuildDoesNotMutateDesign` enforces it.
- Go tests use the standard library `testing` package only. Tests that evaluate a DSL must not call `t.Parallel`.
