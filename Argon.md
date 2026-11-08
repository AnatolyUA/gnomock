# Upgrade Branch Review (`5ce19d1...HEAD`)

## 1. Summary

This is a well-structured, disciplined upgrade branch: the 87 commits are small and self-contained, the Docker/Moby v2 and database/cloud SDK migrations preserve the public Go API without deleting exported symbols, and the new unit tests (`docker_internal_test.go`, `image_internal_test.go`, `logs_internal_test.go`, `pull_internal_test.go`, `swagger_test.go`, `start_internal_test.go`, and per-preset `image_test.go`) provide strong regression coverage. I would **not merge it strictly as-is** only because the 618-line temporary `PLAN.md` scratchpad is still committed on the branch (`PLAN.md:448` leaves `- [ ] Delete PLAN.md.` unchecked) and is referenced by 18 test files. Aside from removing `PLAN.md`, there are no blockers—only a few minor issues in CI workspace package patterns, `WithOptions` merge semantics vs. `docs/UPGRADE-NOTES.md`, and documentation/testdata consistency that are quick to clean up before merging.

---

## 2. Findings

### Major

#### 1. Temporary working document `PLAN.md` is still committed and referenced in 18 test files
- **Location:** `PLAN.md:3`, `PLAN.md:448`, and `preset/*/image_test.go:15` (all 18 presets, e.g., `preset/postgres/image_test.go:15`)
- **What is wrong:** `PLAN.md:3` states `> Temporary working document. Delete it once the work is finished.`, and the final checklist item at `PLAN.md:448` (`- [ ] Delete PLAN.md.`) is still unchecked. In addition, the doc comment on `TestPreset_Image` in all 18 `preset/<name>/image_test.go:15` files reads:
  ```go
  // expected strings are hardcoded on purpose, so that changing a default image
  // (PLAN.md Phase 6) has to update this test deliberately.
  ```
  **Scenario:** Merging the branch as-is lands a 618-line temporary planning document onto `master`, and once `PLAN.md` is deleted, 18 test files will reference a nonexistent file.
- **Suggested fix:** Delete `PLAN.md` and remove `(PLAN.md Phase 6)` from the `TestPreset_Image` comment across `preset/*/image_test.go:15`.
- **Confidence:** High

---

### Minor

#### 2. `./...` in CI lint/vet does not cross into `cmd/cleaner`, hiding a `gosec` failure in `cmd/cleaner/main.go`
- **Location:** `.github/workflows/test.yaml:17-23`, `cmd/cleaner/main.go:58`, `go.work:1-6` (commits `ecdd7a3`, `860f8f0`)
- **What is wrong:** Commit `ecdd7a3` added `go.work` (`use (. ./cmd/cleaner)`) so local and CI builds/checks cover `cmd/cleaner` against the local `gnomock` module. However, in Go, the `./...` pattern evaluated from the repo root stops at nested `go.mod` boundaries (`cmd/cleaner/go.mod`) even when `go.work` is active (`go list ./...` does not include `github.com/orlangure/gnomock/cmd/cleaner`; only `go list work` or `./... ./cmd/cleaner/...` does).
  **Scenario:**
  1. In `.github/workflows/test.yaml:23`, `GOARCH=arm64 go vet ./...` silently skips `cmd/cleaner`.
  2. In `.github/workflows/test.yaml:18-21`, `golangci-lint` defaults to `./...` and skips `cmd/cleaner`. Running `golangci-lint` on `./cmd/cleaner/...` fails on `cmd/cleaner/main.go:58` (`log.Fatalln(http.ListenAndServe(":8008", nil))`) with `gosec` `G114: Use of net/http serve function that has no support for setting timeouts`—the same check that required `// nolint: gosec` in `cmd/server/main.go:38`.
- **Suggested fix:** Use `GOARCH=arm64 go vet work` (or `GOARCH=arm64 go vet ./... ./cmd/cleaner/...`) in `.github/workflows/test.yaml:23`, pass `./... ./cmd/cleaner/...` in `golangci-lint-action` `args` in `.github/workflows/test.yaml:21`, and add `// nolint: gosec` (or a `http.Server` with `ReadHeaderTimeout`) at `cmd/cleaner/main.go:58`.
- **Confidence:** High

#### 3. `WithOptions` unconditionally overwrites `Debug` and `ContainerName` when unset, contradicting `docs/UPGRADE-NOTES.md`
- **Location:** `options.go:113-114`, `options.go:121-122`, `docs/UPGRADE-NOTES.md:21-25` (commit `b2db5d4`)
- **What is wrong:** `applySetOptions` (`options.go:140-177`) applies all 11 newly honored fields only when non-zero (including the 4 boolean fields `Privileged`, `DisableAutoCleanup`, `UseLocalImagesFirst`, `Reuse` and the 3 string fields `Auth`, `CustomImage`, `User`). However, `WithOptions` (`options.go:121-122`) still unconditionally executes:
  ```go
  o.Debug = options.Debug
  o.ContainerName = options.ContainerName
  ```
  Meanwhile, `docs/UPGRADE-NOTES.md:25` states without qualification: *"They are now applied, also for Go callers that pass a populated `Options` to `WithOptions`. Unset fields don't override options applied earlier."*
  **Scenario:** A Go caller who runs:
  ```go
  gnomock.Start(
      p,
      gnomock.WithDebugMode(),
      gnomock.WithContainerName("my-db"),
      gnomock.WithOptions(&gnomock.Options{Privileged: true}),
  )
  ```
  expects `Debug: true` and `ContainerName: "my-db"` to be preserved because neither field was set on the `Options` struct passed to `WithOptions`. Instead, `WithOptions` resets `o.Debug` to `false` and `o.ContainerName` to `""`.
- **Suggested fix:** Either change `options.go:121-122` to `o.Debug = o.Debug || options.Debug` and `if options.ContainerName != "" { o.ContainerName = options.ContainerName }` so all fields on `Options` follow the same non-zero merge rule, or explicitly note the `Debug`/`ContainerName` exception in `docs/UPGRADE-NOTES.md:25`.
- **Confidence:** High

#### 4. `docs/UPGRADE-NOTES.md` omits `redis` and `mongo` from "Init and healthcheck honor the start timeout"
- **Location:** `docs/UPGRADE-NOTES.md:16-20`, `preset/redis/preset.go:64-91`, `preset/mongo/preset.go:118-188` (commits `6d93ff5`, `ca05ae4`)
- **What is wrong:** Item 4 in `docs/UPGRADE-NOTES.md:16-20` states:
  > **Init and healthcheck honor the start timeout.** The SQL presets (`postgres`, `mysql`, `mariadb`, `mssql`, `cockroachdb`) run their healthcheck and init queries with the context `gnomock` passes in. A slow init query now fails when the `Start` timeout expires instead of running until it finishes.
  Two non-SQL presets changed behavior in the exact same way on this branch:
  - `preset/redis/preset.go:64-91`: `WithValues` `initf` (`client.Set(ctx, k, v, 0)`) and `healthcheck` (`client.Ping(ctx)`) previously ignored `ctx` (`_ context.Context`) and now use `gnomock`'s `ctx`.
  - `preset/mongo/preset.go:118,131,144,153,168,182`: `setupCollection` previously created `ctx := context.Background()` for `InsertOne(ctx, val)` and ignored `initf`'s `ctx`; it now threads `initf`'s `ctx` through `setupDB` and `setupCollection` to `InsertOne(ctx, val)`.
  **Scenario:** A caller ingesting large initial datasets with `mongo.WithData` or `redis.WithValues` under a tight `gnomock.WithTimeout` will now hit `context.DeadlineExceeded` during `initf`, a behavioral change not mentioned for `mongo` or `redis` in `docs/UPGRADE-NOTES.md`.
- **Suggested fix:** Update `docs/UPGRADE-NOTES.md:16-20` to include `redis` (ping and `WithValues`) and `mongo` (`WithData` inserts) alongside the SQL presets.
- **Confidence:** High

#### 5. `internal/gnomockd/testdata/vault.json` requests EOL Vault `2.1` instead of `2.2`
- **Location:** `internal/gnomockd/testdata/vault.json:3` (commit `d78ac81`)
- **What is wrong:** `PLAN.md:302`, `docs/UPGRADE-NOTES.md:93`, `README.md:139`, and `preset/vault/preset_test.go:23` all list `2.2` as the sole supported/tested Vault version (`2.1 and older EOL`). `PLAN.md:277` also specifies: *"Update `internal/gnomockd/testdata/<preset>.json` `"version"` to a supported tag other than `defaultVersion` (so the HTTP test proves the version field is wired through; **when only one version is tested, use that version**)"* (which `azurite.json` follows by using `"3.37.0"`). Instead, `internal/gnomockd/testdata/vault.json:3` uses `"version": "2.1"`.
  **Scenario:** In the `[preset] vault` CI job, `go test ./preset/vault/...` pulls `hashicorp/vault:2.2`, and `go test ./internal/gnomockd -run TestVault` then pulls a second, EOL image (`hashicorp/vault:2.1`) that is not listed in `README.md` or `docs/UPGRADE-NOTES.md`.
- **Suggested fix:** Change `"version"` in `internal/gnomockd/testdata/vault.json:3` from `"2.1"` to `"2.2"`.
- **Confidence:** High

---

### Nit

#### 6. `cmd/generator` emits unformatted imports in `preset/<name>/preset.go` when `-public` is false
- **Location:** `cmd/generator/main.go:119-145`, `cmd/generator/templates/preset/preset.go.template:10-14` (commit `a726d83`)
- **What is wrong:** Commit `a726d83` added `formatGoFile` (`cmd/generator/main.go:231`) and called it on `cmd/server/presets.go`, but did not call it in `presetFile` (`cmd/generator/main.go:119`). In `cmd/generator/templates/preset/preset.go.template:10-14`, `{{ if .Public -}}` does not trim the preceding newline and tab.
  **Scenario:** Running `go run ./cmd/generator -name Foo -default-port 1234 -image docker.io/org/foo` without `-public` (a usage documented in `CONTRIBUTING.md:104`) writes an `import (...)` block with a trailing `\t\n` blank line before `)`, which fails `gofmt` / `gofumpt` / `goimports`.
- **Suggested fix:** Call `formatGoFile(presetFile)` in `presetFile` whenever `strings.HasSuffix(file, ".go")` (or use `{{- if .Public }}` in `preset.go.template:11`).
- **Confidence:** High

#### 7. Stale k3s godoc default, Splunk OpenAPI example, pre-existing MySQL/MariaDB OpenAPI defaults, and a stray CRLF in `codeql-analysis.yml`
- **Location:** `preset/k3s/preset.go:154`, `swagger/swagger.yaml:777,818,823,828,1026`, `.github/workflows/codeql-analysis.yml:57`
- **What is wrong:**
  1. `preset/k3s/preset.go:154`: `Preset()` godoc comment still says `// By default, this preset sets up k3s v1.19.3.` while `defaultVersion` is `"v1.37.2-k3s1"` (the package comment at `preset/k3s/preset.go:20` was updated to `v1.37.2-k3s1`).
  2. `swagger/swagger.yaml:1026`: `splunk.version` updated `default: '10.4'` on line 1025, but left `example: 8.0.2.1` on line 1026 (whereas `rabbitmq`, `kafka`, `elastic`, and `kubernetes` updated both `default:` and `example:`).
  3. `swagger/swagger.yaml:777` and `swagger/swagger.yaml:818,823,828` (pre-existing, missed during the Swagger audit): `mysql.password` has `default: gnomick` in `swagger.yaml` vs. `defaultPassword = "gnomock"` in `preset/mysql/preset.go:20`; `mariadb` has `db: default: mydb`, `user: default: gnomock`, `password: default: gnoria` in `swagger.yaml` vs. `defaultDatabase = "books"`, `defaultUser = "Sherlock"`, `defaultPassword = "Holmes"` in `preset/mariadb/preset.go:19-21`.
  4. `.github/workflows/codeql-analysis.yml:57` (commit `820311a`): `uses: github/codeql-action/autobuild@v4\r` has a trailing CRLF (`\r`) carriage return on a single line in an otherwise LF-only file.
- **Suggested fix:** Update `preset/k3s/preset.go:154` to `v1.37.2-k3s1`, `swagger/swagger.yaml:1026` to `example: '10.4'`, align the `mysql` and `mariadb` default credentials in `swagger/swagger.yaml` with their `preset.go` constants, and strip `\r` on `.github/workflows/codeql-analysis.yml:57`.
- **Confidence:** High

---

## 3. Docs/Code Mismatches

- **`docs/UPGRADE-NOTES.md:16-20` vs. `preset/redis/preset.go:64-91` and `preset/mongo/preset.go:118-188`:** `UPGRADE-NOTES.md` states that the 5 SQL presets now honor the `Start` timeout context in `healthcheck` and `initf`, omitting that `redis` (`Ping` in `healthcheck` and `Set` in `WithValues` `initf`) and `mongo` (`InsertOne` in `WithData` `initf`) now honor `gnomock`'s `Start` timeout context as well.
- **`docs/UPGRADE-NOTES.md:25` vs. `options.go:113-114,121-122`:** `UPGRADE-NOTES.md` says *"Unset fields don't override options applied earlier"* for `WithOptions`, whereas `options.go:121-122` unconditionally overwrites `Debug` and `ContainerName` even when unset (`false` / `""`).
- **`preset/k3s/preset.go:154` vs. `preset/k3s/preset.go:20,31`:** `k3s.Preset()` godoc says `By default, this preset sets up k3s v1.19.3`, whereas `defaultVersion` is `v1.37.2-k3s1`.
- **`docs/UPGRADE-NOTES.md:93` & `README.md:139` vs. `internal/gnomockd/testdata/vault.json:3`:** Docs and `PLAN.md:302` list `2.2` as the only tested Vault version (`2.1` is EOL), while `vault.json` tests `"2.1"`. Similarly, `internal/gnomockd/testdata/memcached.json:10` uses `"1.6.45-alpine"` while `README.md:126` and `docs/UPGRADE-NOTES.md:91` list `"1.6.45"`.
- **`swagger/swagger.yaml:777,818,823,828` vs. `preset/mysql/preset.go:20` & `preset/mariadb/preset.go:19-21`:** Swagger lists `default: gnomick` for `mysql.password` (code: `"gnomock"`) and `default: mydb` / `gnomock` / `gnoria` for `mariadb` `db` / `user` / `password` (code: `"books"` / `"Sherlock"` / `"Holmes"`).
- **`swagger/swagger.yaml:1026` vs. `preset/splunk/preset.go:20`:** `splunk.version` has `default: '10.4'` but still carries `example: 8.0.2.1`.

---

## 4. Questions for the Author

1. **`WithOptions` behavior for `Debug` and `ContainerName` (`options.go:121-122`):** Was preserving the unconditional overwrite of `Debug` and `ContainerName` when zero-valued intentional for backward compatibility with existing tests, or should they follow the same "non-zero wins" rule as the other 4 boolean fields and 3 string fields in `applySetOptions`?
2. **`internal/gnomockd/testdata/vault.json` (`"version": "2.1"`) and `memcached.json` (`"1.6.45-alpine"`):** Was `"2.1"` chosen in `vault.json` specifically so the HTTP test wouldn't use the same string as `defaultVersion` (`"2.2"`), and if so, should `2.1` (and `1.6.45-alpine`) be listed in the tested versions column or should `vault.json` use `"2.2"` per `PLAN.md:277`?

---

## 5. Checked and Fine

- **Public Go API compatibility across all packages:**
  - Verified that no exported types, functions, methods, struct fields, or constants were deleted or had incompatible signature changes across `gnomock` and all 18 `preset/*` packages.
  - `gnomock.ErrEnvVarNoSplit` (`options.go:19`) and `kafka.ZooKeeperPort` (`preset/kafka/preset.go:25`) are retained and marked `// Deprecated:` with clear explanations.
  - `splunk.Ingest` (`preset/splunk/init.go:27`) keeps its exact signature while now honoring `ctx` cancellation during event formatting (`preset/splunk/init.go:38-44`) and HTTP request creation (`preset/splunk/init.go:93`).
  - `redis.WithValues(map[string]any)` (`preset/redis/options.go:8`) is type-identical to `map[string]interface{}`.
- **Moby v2 (`github.com/moby/moby/client` v0.6.2 & `github.com/moby/moby/api` v1.56.1) migration (`docker.go`):**
  - Port exposure (`exposedPorts`), port bindings (`portBindings`), bound port inspection (`boundNamedPorts`), gateway lookup (`containerGateway`), image pull stream error propagation (`jsonmessage.DisplayStream`), duplicate container conflict detection (`cerrdefs.IsConflict` + `duplicateContainerRegexp`), and removal-in-progress handling (`isDeletionAlreadyInProgessError`) are all sound and well-covered by unit tests in `docker_internal_test.go`, `image_internal_test.go`, `logs_internal_test.go`, `pull_internal_test.go`, and `sidecar_internal_test.go`.
- **`gnomockd` HTTP router and JSON/OpenAPI contract (`internal/gnomockd/*`, `swagger/swagger.yaml`):**
  - The move from `gorilla/mux` to `net/http.ServeMux` (`POST /start/{preset}`, `POST /stop`) in `internal/gnomockd/gnomockd.go:10-16` and the `405 Method Not Allowed` behavior change are accurately documented in `docs/UPGRADE-NOTES.md:45-48` and tested in `internal/gnomockd/gnomockd_test.go:102-122`.
  - Every `gnomock.Options` JSON tag, preset struct JSON tag (including `k3s`'s untagged `Port`, `UseDynamicPort`, `K3sServerFlags` fields and `localstack`'s new `auth_token` field), container response schema, and error response schema (`invalid-start-request`, `start-failed`, `stop-failed`, `invalid-stop-request`) matches `swagger/swagger.yaml` and is enforced by `internal/gnomockd/swagger_test.go` and `internal/errors/errors_json_test.go`.
- **Preset default versions, arm64 image unification, and test matrices:**
  - Verified all 18 presets' `defaultVersion` constants, `TestPreset_Image` expectations, `preset_test.go` version slices, `README.md` table rows, `docs/UPGRADE-NOTES.md` table rows, and per-preset READMEs; all 18 `defaultVersion` values and preset test matrices match the tables in `PLAN.md` and `docs/UPGRADE-NOTES.md`.
  - Verified that `preset/mysql/preset.go` and `preset/influxdb/preset.go` no longer branch on `runtime.GOARCH == "arm64"`, and `internal/mysqllog/mysqllog.go` guards `mysql.SetLogger` with `sync.Once` to eliminate the data race when `mysql` and `mariadb` run concurrently.
- **Dockerfiles, cross-compilation, and `GOWORK=off` builds:**
  - Verified `go build ./...` and `go vet ./...` with both `go.work` enabled and `GOWORK=off` on `amd64` and `GOARCH=arm64`, as well as `(cd cmd/cleaner && GOWORK=off go build .)`.
  - Both `Dockerfile` (using `GOWORK=off`) and `cmd/cleaner/Dockerfile` (building from repo root with `go.work`) use `FROM --platform=$BUILDPLATFORM golang:1.27-alpine` with `GOOS=$TARGETOS GOARCH=$TARGETARCH`, cross-compiling cleanly without QEMU.
