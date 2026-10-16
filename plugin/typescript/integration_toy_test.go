package typescript

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// goaVersion is the goa release the repository pins; the generator binary
// must match the module version or it fails to compile.
const goaVersion = "v3.34.0"

// toyToken is the credential the toy server is told to accept.
const toyToken = "toy-token"

// TestToyExample_GenerateAndRun runs the whole pipeline on examples/toy:
// `goa gen` with the plugin linked in, a TypeScript compile of the generated
// client, then the vitest suite against the toy's own goa server.
//
// It needs node and pnpm. Without them it is skipped, except in CI where they
// are required. `go test -short` always skips it.
func TestToyExample_GenerateAndRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the toy pipeline in short mode")
	}
	for _, tool := range []string{"node", "pnpm"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s is required in CI: %v", tool, err)
			}
			t.Skipf("%s not found in PATH: install node and pnpm to run the toy pipeline", tool)
		}
	}

	toy := filepath.Join(mustRepoRoot(t), "examples", "toy")

	// Generate code into the toy module using the standard Goa tool.
	run(t, toy, nil, "go", "run", "goa.design/goa/v3/cmd/goa@"+goaVersion, "gen",
		"github.com/crossnokaye/goa-typescript/examples/toy/design", "-o", ".")

	for _, rel := range []string{
		"gen/ts/package.json",
		"gen/ts/tsconfig.json",
		"gen/ts/src/index.ts",
		"gen/ts/src/types.ts",
		"gen/ts/src/runtime/index.ts",
		"gen/ts/src/runtime/sse.ts",
		"gen/ts/src/toy/index.ts",
		"gen/ts/src/toy/stream-things-sse.ts",
		"gen/ts/src/toy/stream-wrapped-sse.ts",
		"gen/ts/src/toy/stream-post-sse.ts",
		"gen/ts/src/toy/stream-text-sse.ts",
		"gen/ts/src/toy/stream-count-sse.ts",
		"gen/ts/src/toy/stream-viewed-sse.ts",
		"gen/ts/src/toy/stream-public-sse.ts",
		"gen/ts/src/toy/stream-keyed-sse.ts",
		"gen/ts/src/toy/get-thing.ts",
		"gen/ts/src/toy/get-thing.query.ts",
		"gen/ts/src/toy/head-thing.ts",
		"gen/ts/src/toy/head-thing.query.ts",
		"gen/ts/src/toy/create-thing.query.ts",
		"gen/ts/src/toy/stream-things-sse.query.ts",
		"gen/ts/src/toy/watch-ticks.query.ts",
		"gen/ts/src/toy/ping.query.ts",
		"gen/ts/src/toy/get-thing-viewed.ts",
		"gen/ts/src/toy/list-things.ts",
		"gen/ts/src/toy/create-thing.ts",
		"gen/ts/src/toy/upsert-thing.ts",
		"gen/ts/src/toy/find-thing.ts",
		"gen/ts/src/toy/delete-thing.ts",
		"gen/ts/src/toy/download-thing.ts",
		"gen/ts/src/toy/upload-thing.ts",
		"gen/ts/src/toy/stream-or-list.ts",
		"gen/ts/src/toy/thing-count.ts",
		"gen/ts/src/toy/thing-blob.ts",
		"gen/ts/src/echo/index.ts",
		"gen/ts/src/echo/stream-things-sse.ts",
	} {
		if _, err := os.Stat(filepath.Join(toy, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected generated file %s: %v", rel, err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	// `goa gen` wipes gen/, node_modules included, so install every time. The
	// toy's lockfile is not committed: gen/ts is a workspace member that only
	// exists after generation.
	run(t, toy, nil, "pnpm", "install", "--no-frozen-lockfile")

	// Compile the tests and the generated package with the pinned TypeScript.
	run(t, toy, nil, "pnpm", "--filter", "toy-e2e", "type-check")

	// Build and start the toy server, which imports the generated Go code.
	bin := filepath.Join(t.TempDir(), "toy")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	run(t, toy, nil, "go", "build", "-o", bin, "./cmd/toy")
	baseURL := startToyServer(t, bin)

	run(t, toy, []string{"TOY_BASE_URL=" + baseURL, "TOY_TOKEN=" + toyToken},
		"pnpm", "--filter", "toy-e2e", "test")
}

// startToyServer starts the toy server on a free port and returns its base
// URL, read from the line the server prints once it listens.
func startToyServer(t *testing.T, bin string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "-http-addr", "127.0.0.1:0", "-token", toyToken)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	var (
		mu     sync.Mutex
		output strings.Builder
	)
	cmd.Stderr = writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return output.Write(p)
	})
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start toy server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		mu.Lock()
		defer mu.Unlock()
		if t.Failed() && output.Len() > 0 {
			t.Logf("toy server output:\n%s", output.String())
		}
	})

	const prefix = "listening on "
	found := make(chan string, 1)
	go func() {
		defer close(found)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			mu.Lock()
			output.WriteString(line + "\n")
			mu.Unlock()
			if url, ok := strings.CutPrefix(line, prefix); ok {
				found <- strings.TrimSpace(url)
				break
			}
		}
		// Keep draining so the server never blocks on a full pipe.
		_, _ = io.Copy(io.Discard, stdout)
	}()

	select {
	case url, ok := <-found:
		if !ok || url == "" {
			t.Fatalf("toy server exited before printing %q", prefix)
		}
		return url
	case <-time.After(10 * time.Second):
		t.Fatalf("toy server did not print %q within 10s", prefix)
	}
	return ""
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(dir, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected go.mod at repo root %q: %v", root, err)
	}
	return root
}

func run(t *testing.T, dir string, env []string, exe string, args ...string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOFLAGS=-mod=mod",
		"GOWORK=off",
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %s\n%s\n%v", exe, strings.Join(args, " "), string(out), err)
	}
}
