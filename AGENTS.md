# Repository Guidelines

## Scope

These instructions apply to the entire repository. A more specific
`AGENTS.md` in a subdirectory may refine them for that subtree.

`vault-go-config` is a public Go library that builds typed configuration
structs from environment variables, HashiCorp Vault KV v2, JSON, and
caller-provided defaults. Treat public identifiers, struct-tag semantics,
source precedence, environment-variable names, authentication order, error
categories, retry behavior, and cache isolation as compatibility-sensitive
API.

## Repository Map

- Root `*.go` files: public `vltconfig` package and focused runtime components.
- Root `*_test.go` files: offline unit and regression tests.
- `tests/integration/`: opt-in tests against disposable infrastructure.
- `examples/`: runnable, secret-free usage examples.
- `scripts/`: canonical local quality and integration commands.
- `.githooks/`: mandatory committed pre-commit and pre-push protection.
- `.github/workflows/`: pull-request validation and release automation.
- `go.mod` and `go.sum`: module, runtime, and tool dependencies.

Do not rename the module or package, move the public API, or introduce a
second configuration framework unless the task explicitly calls for a
breaking change.

## Public Behavior

The configuration priority is, from highest to lowest:

1. Process environment variables.
2. HashiCorp Vault KV v2.
3. JSON configuration.
4. Existing values in the destination struct.

Preserve this order unless a deliberately breaking change is requested. Any
change requires precedence regression tests, README updates, and release
notes.

Vault authentication tries token, AppRole, and userpass in that order. An
entirely absent Vault access environment skips Vault. Any partial or invalid
configured Vault is an error under the default strict policy. A successful
partial Vault payload must still be completed by lower-priority sources.

The main public entry points are the generic one-shot `Load`, the package-level
`LoadInto`, and a reusable `Loader`. Loading into an existing struct uses it as
the defaults source and must not mutate it on failure.

## Environment and Dependencies

Go 1.25 is the minimum language and standard-library baseline. Go 1.25 and
1.26 are blocking CI targets until the supported Go release window changes.
Do not introduce syntax or APIs unavailable in Go 1.25.

Use Go modules as the only dependency manager. Add runtime dependencies
deliberately, keep them minimal, and run:

```bash
go mod tidy
go mod verify
```

Staticcheck and govulncheck are Go tool dependencies. Do not replace pinned
tool directives with machine-global assumptions.

Canonical commands are:

```bash
./scripts/fast-check.sh
./scripts/quality.sh
./scripts/test-integration.sh
```

Committed hooks must remain enabled through `core.hooksPath=.githooks`. Do not
bypass a hook or lower a gate to make a change pass.

## Go Code Quality

- Format every Go file with `gofmt`.
- Keep packages small, cohesive, and free of import cycles.
- Prefer explicit constructors and interfaces at I/O boundaries.
- Accept `context.Context` as the first argument for blocking operations and
  honor cancellation promptly.
- Return errors; do not panic for configuration, I/O, or validation failures.
- Use `errors.Is` and `errors.As` compatible wrapping for public categories.
- Avoid package-global mutable state. A reusable `Loader` owns collaborators
  and cache state.
- Keep exported declarations documented and stable.
- Do not use reflection beyond the schema/decode boundary when ordinary typed
  code is clearer.
- Keep test seams focused; do not expose implementation details only for
  tests.

## Struct and Source Design

The `json` tag owns canonical configuration names. The `env` tag overrides an
exact environment variable name, and `vlt:"required"` marks an externally
required path. Preserve the `__` nested env delimiter.

Requiredness is based on source presence, not Go zero values. Explicit
`false`, `0`, and empty strings count as present. Programmatic defaults belong
in ordinary Go constructors and should not also be marked required.

Compile and validate a struct schema before source I/O. Reject recursive
schemas, unsupported map keys, duplicate canonical paths, and duplicate env
names deterministically.

Deep-merge object values by leaf. Higher-priority scalars and collections
replace lower values atomically. Keep null, unknown-key, missing-file, and
malformed-input semantics explicit and covered by tests.

Each source loads exactly one kind of data. Source orchestration and priority
belong in the loader, not inside individual sources. New optional sources must
be independently selectable and must not change existing priority silently.

## Vault Security

Never place real credentials in source files, tests, examples, README text,
generated reports, logs, commits, issues, or tool output. This includes Vault
tokens, passwords, role IDs, secret IDs, and secret payload values.

- Use unmistakably fake values in examples.
- Never interpolate credentials, environment values, response bodies, or
  assembled configuration into errors or logs.
- Never include raw credentials in cache keys; use a keyed fingerprint.
- Do not log complete Vault addresses when they may contain user information.
- Do not expose an upstream error string unless it is known to be value-free.
- Do not configure application-wide logging from library code.
- Use `log/slog` only through an optional caller-owned logger.
- Do not add live credentials to CI or integration fixtures.

If a real credential is discovered, stop exposing it, report only its
location, rotate it, and assess Git history. Removing the current file is not
sufficient.

## Vault Reliability and Cache

Retry only confirmed transient transport failures, timeouts, HTTP 429, and
HTTP 5xx. Authentication rejection, authorization, invalid paths, invalid
configuration, malformed responses, decoding, and validation are not
retryable. Retry waits must respect context cancellation and be replaceable
in tests.

Vault caches must be bounded, thread-safe, and owned by one `Loader`. Use a
monotonic clock, deep-copy stored and returned values, include every isolation
dimension in the key, and provide explicit invalidation. Test expiry, LRU
eviction, credential rotation, concurrency, and mutation isolation with the
race detector.

Disable retry behavior built into third-party Vault clients when the library
owns the retry policy, so attempt counts remain exact.

## Testing

Every behavior change requires a regression test. Prefer observable behavior
over internal implementation assertions.

- Keep the default suite offline and deterministic.
- Use `t.Setenv` for environment changes.
- Never depend on test execution order or process-global cache state.
- Replace retry sleeps, clocks, filesystem access, and Vault calls with focused
  collaborators where necessary.
- Assert specific errors with `errors.Is` or `errors.As`.
- Test nested and collection-valued settings, defaults, aliases, precedence,
  optional sources, invalid types, nulls, unknown keys, and permission errors.
- Assert that canary credentials and payload values never appear in errors or
  logs.
- Run concurrency-sensitive cache and loader tests with `go test -race`.
- Keep real Vault coverage opt-in and use only a disposable development
  server.

The supported Go matrix runs the same unit suite. Coverage measures the public
package and must not exclude difficult security or error paths merely to
increase the percentage.

## CI/CD and Releases

Pull-request CI must include:

- gofmt verification;
- `go vet`;
- unit tests on every supported Go minor;
- race tests and coverage on one canonical version;
- Staticcheck and govulncheck;
- module verification and build;
- example build and a JSON-only smoke test;
- opt-in or isolated disposable Vault integration coverage.

Pin third-party GitHub Actions to immutable revisions where practical and use
automated dependency updates for both Go modules and Actions. Give jobs only
the permissions they need. Publishing and release automation must never run
when blocking quality or compatibility checks fail.

## Documentation and Examples

README is the canonical public documentation. Keep installation, public API,
source priority, tags, nesting, defaults, environment variables, Vault auth,
failure behavior, retry/cache policy, security guidance, and examples aligned
with code.

Examples must build, run without live infrastructure unless explicitly marked
as integration examples, use fake credentials, and never print a
secret-bearing configuration.

Repository-local design notes, plans, audits, and dossiers belong under the
ignored `docs/` directory. Never force-add that directory. User-facing facts
must not exist only in ignored documentation.

## Change Discipline

- Inspect `git status` before editing and preserve unrelated user work.
- Use `rg` and `rg --files` for repository searches.
- Keep each change scoped to the requested behavior.
- Preserve public contracts unless a breaking change is explicit.
- Do not use destructive Git commands on user commits.
- Follow the repository's phase and atomic-commit workflow when a task plan
  requires it.
- Do not bypass hooks, skip tests, suppress vulnerabilities, or weaken checks.
- Treat pre-existing failures separately from task-introduced failures and
  report them exactly.

## Completion Checklist

Before declaring work complete:

- Relevant unit and integration tests pass.
- `gofmt`, `go vet`, Staticcheck, and govulncheck pass.
- Race tests and coverage pass.
- `go mod tidy` produces no unexplained changes and `go mod verify` passes.
- Every example builds and the documented local smoke path runs.
- Security-sensitive values are absent from changes and output.
- README matches every changed public behavior.
- `git diff --check` passes.
- Hooks are enabled and pass.
- Ignored `docs/` is not staged.
- Unrelated files remain untouched.

Report commands run, outcomes, commits by phase, and any validation that could
not be completed.
