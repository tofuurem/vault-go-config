# vault-go-config

`vault-go-config` builds a typed Go configuration struct from process
environment variables, HashiCorp Vault KV v2, a JSON file, and ordinary Go
defaults.

It is deliberately small and opinionated:

- source priority is `Env > Vault > JSON > defaults`;
- nested objects merge by leaf instead of replacing the whole section;
- Vault is optional when it is completely unconfigured, but strict once any
  Vault access setting is present;
- token, AppRole, and userpass authentication are supported;
- required fields track external presence, so `false`, `0`, and `""` are valid
  supplied values;
- failed loads never partially mutate the destination;
- retries, typed errors, and a per-loader cache are designed not to expose
  credentials or secret payloads.

The module requires Go 1.25 or newer. Go 1.25 and 1.26 are tested.

## Install

```bash
go get github.com/tofuurem/vault-go-config@latest
```

The module path is `github.com/tofuurem/vault-go-config`; the package name is
`vltconfig`.

## Quick start

Define a normal Go struct:

```go
package main

import (
    "context"
    "fmt"
    "log"

    vltconfig "github.com/tofuurem/vault-go-config"
)

type DatabaseConfig struct {
    Host string `json:"host" vlt:"required"`
    Port int    `json:"port" vlt:"required"`
    SSL  bool   `json:"ssl"`
}

type AppConfig struct {
    ServiceName string         `json:"service_name" vlt:"required"`
    Database    DatabaseConfig `json:"db"`
}

func main() {
    cfg, err := vltconfig.Load[AppConfig](context.Background())
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("starting %s on database port %d\n", cfg.ServiceName, cfg.Database.Port)
}
```

A local `config.json` can contain:

```json
{
  "service_name": "billing-api",
  "db": {
    "host": "127.0.0.1",
    "port": 5432,
    "ssl": false
  }
}
```

Override one nested leaf without replacing the rest:

```bash
DB__HOST=postgres.internal go run .
```

With no Vault access variables present, Vault is skipped and this remains a
fully local JSON-plus-environment workflow.

## API

### One-shot load

```go
cfg, err := vltconfig.Load[AppConfig](ctx)
```

`Load` starts from the zero value of `AppConfig`. Use it when all required
values come from external sources.

### Load over Go defaults

```go
func DefaultConfig() AppConfig {
    return AppConfig{
        Database: DatabaseConfig{Host: "localhost", Port: 5432},
    }
}

cfg := DefaultConfig()
err := vltconfig.LoadInto(ctx, &cfg)
```

Existing struct values are the lowest-priority defaults. There is no string
`default` tag: constructors keep defaults typed and allow maps, slices,
pointers, and nested values. Do not mark a field `vlt:"required"` merely
because it has a default; `required` specifically means externally supplied.

The destination is assigned only after source loading, decoding, required
checks, and custom validation all succeed. On failure, `cfg` is unchanged.

### Reusable loader

```go
loader, err := vltconfig.New(
    vltconfig.WithJSONFile("/etc/my-service/config.json"),
)
if err != nil {
    return err
}

cfg := DefaultConfig()
if err := loader.Load(ctx, &cfg); err != nil {
    return err
}

loader.InvalidateVaultCache()
```

A `Loader` owns its bounded Vault cache and is safe for concurrent `Load`
calls. Package-level helpers create a new one-shot loader, so use `New` when
cache reuse is useful. `InvalidateVaultCache` is safe during concurrent loads.

Available options are:

- `WithJSONFile(path)` — use one exact JSON file;
- `WithVaultFailurePolicy(VaultFailureStrict)` — explicit default behavior;
- `WithVaultFailurePolicy(VaultFailureFallback)` — treat a typed Vault failure
  as an empty source and continue to JSON/defaults.

## Struct mapping

The `json` tag defines the canonical configuration key. Without it, exported
Go field names become snake case. `json:"-"` excludes a field.

```go
type Config struct {
    APIURL   string            // canonical key: api_url
    Database DatabaseConfig    `json:"db"`
    Labels   map[string]string `json:"labels"`
    Internal string            `json:"-"`
}
```

Supported values include nested structs and struct pointers, scalars, arrays,
slices, string-keyed maps, `time.Duration`, and types implementing
`encoding.TextUnmarshaler`. Recursive struct graphs, non-string map keys,
ambiguous tags, duplicate canonical paths, and duplicate effective env names
are rejected before any source I/O.

Anonymous embedded fields require an explicit `json` tag. This keeps the
canonical path unambiguous.

### Environment names

Environment names are derived from the full canonical path:

| Canonical path | Environment variable |
| --- | --- |
| `service_name` | `SERVICE_NAME` |
| `db.host` | `DB__HOST` |
| `db.pool.max_open` | `DB__POOL__MAX_OPEN` |

Override or disable environment loading with an `env` tag:

```go
Password string `json:"password" env:"DATABASE_PASSWORD" vlt:"required"`
LocalOnly string `json:"local_only" env:"-"`
```

Scalar environment values are converted to the destination type. Structs,
maps, slices, arrays, and interfaces use JSON syntax:

```bash
FEATURES='["search","billing"]'
LABELS='{"region":"eu","tier":"api"}'
```

Environment lookup is case-sensitive. Explicit empty strings count as
present and can satisfy a required string field.

### Required fields

`vlt:"required"` requires the canonical path to be explicitly present in Env,
Vault, or JSON:

```go
Enabled bool `json:"enabled" vlt:"required"`
Workers int  `json:"workers" vlt:"required"`
```

`ENABLED=false` and `WORKERS=0` both satisfy the requirement. A default value
alone does not. Missing paths are returned in `*vltconfig.RequiredError`
without their values.

### Custom validation

Implement `Validate() error` on the configuration pointer:

```go
func (cfg *AppConfig) Validate() error {
    if cfg.Database.Port < 1 || cfg.Database.Port > 65535 {
        return errors.New("database port is outside the allowed range")
    }
    return nil
}
```

Validation runs after decoding and required checks. Its failure is wrapped in
`*vltconfig.ValidationError`; `errors.Is` and `errors.As` still work, while
the outer error message does not copy a potentially sensitive validation
message.

## Source behavior

Priority, from highest to lowest:

| Priority | Source |
| ---: | --- |
| 1 | Process environment |
| 2 | Vault KV v2 |
| 3 | JSON |
| 4 | Existing destination values |

Objects and string-keyed maps merge recursively. Scalars, pointers to scalar
values, slices, and arrays are replaced by the higher-priority source. A
successful partial Vault payload can therefore be completed by JSON or
defaults, while environment variables override only the leaves they address.

An explicit JSON `null` replaces a lower value for pointers, maps, slices, and
interfaces. `null` for a non-nullable scalar is an error. Unknown canonical
keys are rejected to catch configuration typos.

### JSON

Resolution order is:

1. `WithJSONFile(path)`, when supplied;
2. `<PYDANTIC_JSON_PATH>/config.json`, when that compatibility variable is
   present;
3. `./config.json`.

A missing implicit `./config.json` skips JSON. An explicitly selected path is
strict: missing files, permission errors, invalid UTF-8, malformed JSON,
multiple JSON documents, and non-object roots return typed errors. JSON is
read-only; the library never generates or rewrites it.

### Vault activation

Vault access configuration uses these exact variables:

| Variable | Meaning |
| --- | --- |
| `VAULT_ADDRESS` | HTTP or HTTPS server address |
| `VAULT_APP_NAME` | KV v2 secret path |
| `VAULT_MOUNT_POINT` | KV v2 mount; default `secret` |
| `VAULT_TOKEN` | Token authentication |
| `VAULT_ROLE_ID` | AppRole role ID |
| `VAULT_SECRET_ID` | AppRole secret ID |
| `VAULT_USERNAME` | userpass username |
| `VAULT_PASSWORD` | userpass password |

If all eight access variables are absent, Vault is not configured and is
skipped. Cache variables alone do not activate it.

If any access variable is present, `VAULT_ADDRESS`, `VAULT_APP_NAME`, and at
least one complete auth method are required. Invalid or partial configuration
fails before network access. A complete method remains usable even if a
secondary method is only partially configured.

Authentication order is stable:

1. token;
2. AppRole;
3. userpass.

The next complete method is tried only after explicit credential rejection.
Network errors, cancellation, malformed responses, and authorization failures
do not silently switch auth methods.

The official HashiCorp client reads the latest version from KV v2. A missing
or deleted path, forbidden access, bad credentials, an invalid response, or
an unavailable configured Vault fails in strict mode. Lower-priority JSON and
defaults do not hide a broken Vault.

`VaultFailureFallback` is an explicit migration escape hatch:

```go
cfg, err := vltconfig.Load[AppConfig](
    ctx,
    vltconfig.WithVaultFailurePolicy(vltconfig.VaultFailureFallback),
)
```

Use it only when continuing without Vault is acceptable. Context cancellation
still stops the load.

### Vault retry and cache

The library makes three total attempts with context-aware waits of two and
four seconds. Only transport failures, timeouts not caused by caller
cancellation, HTTP 429, and HTTP 5xx are retried. Authentication,
authorization, missing paths, configuration, and malformed responses are not.
The underlying Vault client's own retries are disabled so the budget is exact.

Each reusable `Loader` has an independent, thread-safe LRU cache. Defaults and
controls are:

| Variable | Default | Meaning |
| --- | ---: | --- |
| `VAULT_CACHE_DISABLED` | `false` | Accepts `true/false`, `1/0`, or `yes/no` |
| `VAULT_CACHE_TTL` | `300` | Entry lifetime in seconds; `0` bypasses caching |
| `VAULT_CACHE_MAX_ENTRIES` | `128` | Maximum entries for that loader |

Invalid explicit cache settings are configuration errors. Entries expire on a
monotonic clock and are deep-copied on write and read. Cache identity includes
address, mount, path, and an HMAC credential fingerprint made with a random
per-loader key; raw credentials are never cache keys. Credential rotation
naturally causes a miss.

## Errors

Use `errors.Is` for stable categories and `errors.As` for safe typed details:

- `ErrConfig`, `ErrSchema`, `ErrDecode`, `ErrRequired`, `ErrValidation`;
- `ErrJSON`, `ErrJSONNotFound`, `ErrJSONPermission`, `ErrJSONMalformed`,
  `ErrJSONStructure`;
- `ErrVault`, `ErrVaultConfiguration`, `ErrVaultAuthentication`,
  `ErrVaultAuthorization`, `ErrVaultPathNotFound`, `ErrVaultUnavailable`,
  `ErrVaultResponse`.

```go
var missing *vltconfig.RequiredError
if errors.As(err, &missing) {
    log.Printf("missing configuration paths: %v", missing.Paths)
}
if errors.Is(err, vltconfig.ErrVaultUnavailable) {
    // Apply application-specific startup or health-check policy.
}
```

Typed errors contain safe paths, operations, and upstream error types where
useful. They do not retain Vault response bodies, source values, complete
configuration structs, or raw credentials.

## Security and operational notes

- Never commit `config.json`, Vault credentials, or generated secret
  templates. The repository-level `config.json` path is ignored by default.
- Prefer a policy-scoped token, AppRole, or userpass identity with read access
  only to the required KV v2 path.
- Do not print a complete configuration struct if it contains secrets.
- The library does not install log handlers and emits no logs by default.
- Use a context deadline around startup if Vault availability must be bounded.
- Runtime online refresh is intentionally not provided: load at a controlled
  application boundary and decide explicitly when replacing live state is
  safe.

## Examples and development

Runnable examples live in `examples/basic`, `examples/defaults`, and
`examples/nested`. They use tracked, secret-free JSON fixtures and do not
require Vault:

```bash
go run ./examples/basic
go run ./examples/defaults
go run ./examples/nested
```

Project checks:

```bash
./scripts/fast-check.sh
./scripts/quality.sh
./scripts/test-integration.sh
```

The integration script starts and removes a disposable Vault dev container;
it never targets a persistent server. Repository-local plans and audit notes
live under ignored `docs/` and are not part of the public repository.

## Differences from the Python library

The source precedence, Vault environment names, auth order, strict failure
semantics, retries, and cache controls intentionally mirror Python
`vltconfig`. Go-specific differences are explicit `Load` calls, constructor
defaults, `DB__HOST` nested env names, struct tags, `Validate() error`, and
`./config.json` as the implicit local file.

CLI schema export, template/check/plan/apply workflows, Vault writes, YAML,
TOML, dotenv, file-secret directories, and automatic live refresh are not part
of this initial runtime release.

## License

MIT. See [LICENSE](LICENSE).
