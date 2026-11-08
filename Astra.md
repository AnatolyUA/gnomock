# Review of `claude/blissful-ptolemy-5pkco6`

## 1. Summary

I would not merge this branch as is: the new LocalStack token handling exposes credentials through debug logging, and the context changes do not yet reliably bound all database operations or clean up failed connections. The ordinary success paths, dependency adaptations, and agreed option semantics are generally coherent, and the added tests materially improve coverage. The remaining weaknesses are concentrated in cancellation, failed startup, and log-stream shutdown; some are inherited defects exposed by this review rather than regressions, and are identified below. This review covers `5ce19d1...99c5139`, including the surrounding lifecycle code and relevant pinned dependency implementations; no test suites, containers, builds, or vet runs were started, and the existing uncommitted Splunk edits were excluded.

## 2. Findings

### Major — LocalStack tokens and newly honored registry credentials reach debug logs

**Location:** `preset/localstack/preset.go:88`; logging sink: `gnomock.go:100`; registry credential application: `options.go:166`.

**Defect and scenario:** Set `LOCALSTACK_AUTH_TOKEN` on the host and start a LocalStack preset with `WithDebugMode()`, or submit a gnomockd request with `"debug":true`. The fallback token is appended to `Options.Env`, and `StartCustom` logs the entire configuration, including that environment entry. This happens before Docker connection/startup, so even a failed start reveals the token. Avoiding storage in `P.AuthToken` protects preset serialization but does not protect this log. Similarly, gnomockd now copies `options.auth` into the configuration and logs the reversible base64 credential string.

**Suggested fix:** Log a dedicated sanitized configuration view, omitting `Auth` and redacting secret environment values, including `LOCALSTACK_AUTH_TOKEN`. Preserve the real values only for the Docker request. Add assertions against captured debug output for both explicit and fallback tokens and registry credentials.

**Confidence:** High. The logging sink predates this branch; the automatic host-token propagation and HTTP registry-auth propagation are new.

### Major — A long container log line can exhaust gnomockd memory and deadlock failed startup

**Location:** `internal/gnomockd/start.go:69`, `internal/gnomockd/start.go:90`; cleanup wait: `gnomock.go:172`.

**Defect and scenario:** A container emits a line larger than `bufio.Scanner`'s default approximately 64 KiB limit before becoming healthy. `Scan` stops, closes `receivedLogLines`, and leaves the pipe reader open. The collector receives from the closed channel without checking `ok`, repeatedly appending empty strings and consuming memory. Meanwhile, the Docker log copier can remain blocked writing the unconsumed remainder into the pipe. On startup timeout, `Stop` waits for that copier; the handler closes `started` only after `Start` returns, producing a shutdown cycle. Even ordinary shutdown can strand the scanner on its unconditional `receivedLogLines <- ...` if the collector selects `done` first.

**Suggested fix:** Handle channel closure explicitly, close the reader on every scanner exit, and make sends cancellation-aware. Bound retained log bytes and define how oversized lines are truncated or drained; increasing the scanner limit alone does not fix the shutdown bugs. Coordinate collector shutdown before waiting for the log copier.

**Confidence:** High. This is inherited handler code, now also reachable through the newly honored arbitrary `cmd`/`entrypoint`/`customImage` options; it is not caused by the router replacement.

### Major — PostgreSQL and CockroachDB connection handshakes remain unbounded

**Location:** `preset/postgres/preset.go:177`, `preset/postgres/preset.go:189`; `preset/cockroachdb/preset.go:151`, `preset/cockroachdb/preset.go:162`.

**Defect and scenario:** The new `PingContext(ctx)` calls do not fully establish the claimed timeout behavior with the pinned `lib/pq v1.12.3`. Its `Connector.open` uses the context for TCP dialing, then performs the PostgreSQL startup/authentication handshake synchronously. Its `dial` sets a socket deadline only when `connect_timeout` is configured, which these connection strings omit. A server/proxy that accepts TCP and then stalls during authentication therefore keeps the healthcheck or init blocked beyond `WithTimeout`; `g.wait` cannot observe cancellation while inside that callback, and cleanup never starts.

**Suggested fix:** Keep lib/pq, but configure a connector/dialer that bounds the established socket by the startup deadline and closes it on cancellation, with the cancellation callback released when the connection closes. A finite `connect_timeout` is a useful fallback but alone does not implement prompt cancellation of the supplied context. Cover an accepted-but-silent TCP connection, not merely a refused connection or a slow SQL query.

**Confidence:** High. The unbounded handshake is inherited; replacing `Ping` with `PingContext` leaves this part of the branch's timeout work incomplete. Evidence is in the pinned dependency's `conn.go`, `Connector.open` and `dial`, rather than an assumption about the SQL context API.

### Major — Startup does not own the sidecar wait or cleanup of partially started containers

**Location:** `docker.go:144`, `docker.go:158`, `docker.go:169`; outer cleanup registration: `gnomock.go:123`.

**Defect and scenarios:**

- The main container gets its ports quickly, but pulling/starting the cleaner stalls. The unconditional receive from `sidecarChan` cannot observe the main startup context. The cleaner starts under a separate background context and its default ten-minute timeout, so a caller's short timeout or canceled HTTP request can remain blocked for minutes. Its failure is then silently discarded.
- With `DisableAutoCleanup` enabled, the main container starts successfully but the context expires before network inspection finishes. `startContainer` returns `nil, error` without stopping/removing the running container. The outer `newContainer` cleanup defer has not yet been installed. Docker `AutoRemove` does not stop a running container, so this leaves an orphan and returns no container handle for cleanup. The same ownership gap exists when the cleaner has not established its notification connection yet.

**Suggested fix:** Register cleanup ownership immediately after creation and remove a failed, non-debug startup using a separate bounded cleanup context. Make sidecar startup and the channel wait observe the startup context, propagate setup failures, and cancel unfinished sidecar startup on failure. Keep the successful cleaner notification's lifetime separate from the startup deadline so it continues protecting a returned container.

**Confidence:** High. These are inherited lifecycle defects; the branch's sidecar tests exercise successful explicit `Stop`, and newly honoring `disable_cleanup` exposes the second scenario over HTTP too.

### Major — Canceled SQL init leaks the database pool's goroutine

**Location:** `preset/mysql/preset.go:109`, `preset/mysql/preset.go:149`; also `preset/mariadb/preset.go:106`, `preset/postgres/preset.go:114`, `preset/postgres/preset.go:130`, `preset/cockroachdb/preset.go:109`, `preset/cockroachdb/preset.go:122`.

**Defect and scenario:** These `connect` helpers return the allocated `*sql.DB` alongside a failed `PingContext`. If the deadline expires between the successful healthcheck and init, or during init's new connection, the init callback immediately returns the error without closing that non-nil database. `sql.Open` starts a `connectionOpener` goroutine with its own background lifetime; canceling the ping or stopping the container does not terminate it. Repeated canceled starts in a long-running gnomockd process accumulate leaked pools/goroutines. The healthchecks explicitly close this error-path handle, but the init callbacks do not.

**Suggested fix:** Give each connection helper an unambiguous ownership contract:

```go
if err := db.PingContext(ctx); err != nil {
    _ = db.Close()
    return nil, err
}
return db, nil
```

Apply this to all four affected presets and verify failed init connection attempts leave no pool goroutines.

**Confidence:** High. The failed-ping ownership bug already existed; the new context propagation introduces cancellation as another direct trigger and should close that path.

### Minor — Redis v9 still ignores the supplied socket deadline

**Location:** `preset/redis/preset.go:61`, `preset/redis/preset.go:89`.

**Defect and scenario:** Both clients leave `ContextTimeoutEnabled` false. In the pinned v9.23.0 driver, `baseClient.context` explicitly substitutes `context.Background()` in this configuration for socket reads/writes. If a final init `SET` is sent shortly before the start deadline and its reply arrives one second later, it can return success after the deadline; a silent server instead holds the operation until the driver's own read timeout. Merely supplying `ctx` to `Set`/`Ping` does not provide the intended deadline behavior.

**Suggested fix:** Enable `ContextTimeoutEnabled` in both client constructors and ensure cancellation during an in-flight operation also closes/unblocks the dedicated client. Check the context before reporting successful initialization. Test deadlines and explicit cancellation separately against a controlled delayed RESP endpoint.

**Confidence:** High. This is incomplete context handling in the new v9 integration, not a claim that every driver operation ignores context.

### Minor — New image-lookup tests enshrine false matches across repositories

**Location:** `image_internal_test.go:113`, `image_internal_test.go:129`; implementation: `docker.go:93`.

**Defect and scenario:** With only `postgres:16` cached, request `registry.example.com/library/postgres:16` using `WithUseLocalImagesFirst`. The suffix comparison reports that the requested image exists and skips pulling it. Container creation still asks Docker for the fully qualified private-registry image and fails with “No such image”; it does not substitute the cached Docker Hub image. Namespace suffixes such as `foolibrary/postgres:16` have the same problem. The new tests explicitly require these false positives, so they would reject a correctness fix. This matters particularly now that HTTP callers can combine `customImage` and `use_local_images_first`.

**Suggested fix:** Compare normalized Docker references, including registry, namespace, and tag/digest, or inspect the exact requested reference. Change the cross-registry/namespace expectations to false and add a fake-daemon test verifying that an unrelated cached image does not suppress the pull.

**Confidence:** High. The lookup algorithm is inherited and extracted unchanged on this branch; the assertions preserving its incorrect behavior are new. This is separate from the agreed handling of valid Docker Hub shorthand.

### Minor — The new “does not wait” log-close cases never assert that behavior

**Location:** `logs_internal_test.go:307`.

**Defect and scenario:** When `wantWait` is false, the test releases the worker before observing `done`. A regression that always calls `g.Wait()` and then returns the close error still passes every such case, despite their names claiming an immediate return. The positive wait check also lacks a signal that the close operation has actually begun before its 100 ms observation window.

**Suggested fix:** Signal entry into `fakeReadCloser.Close`. For the no-wait cases, require the close result before releasing the worker, with a bounded failure timeout and cleanup that always releases it. For wait cases, synchronize on that entry signal before checking that completion is blocked.

**Confidence:** High. This is a defect in newly added test coverage, not a demonstrated regression in `closeLogReader` itself.

## 3. Test gaps

- **New gnomockd options:** the golden test stops at applying `WithOptions` to a struct; capture a fake daemon's create/pull requests through the real handler and assert `privileged`, mounts, command, entrypoint, image, user, extra hosts, and registry auth reach the correct API fields.
- **Credential handling:** capture debug logs and startup error responses with sentinel explicit/fallback LocalStack tokens and registry credentials; assert the sentinels are absent while the Docker request retains the required credentials.
- **Context changes:** cancel during SQL connection establishment and init queries, Redis command I/O, and Mongo inserts; assert bounded return, error propagation, and resource closure, including the failure cases above.
- **Pull-stream change:** cancel a fake daemon response mid-stream and verify prompt return and response closure; existing cases cover completed responses only.
- **Mongo loader rewrite:** cover malformed/truncated extended JSON, an empty collection file, insert failure, and descriptor closure on both success and failure; existing new fixture primarily exercises successful decoding.
- **New cleaner workspace/image build:** exercise the locally built cleaner image in the lifecycle tests; current tests pull the upstream cleaner, while the workflow's new cleaner-image job only builds it. Also test loss of the notification connection without calling `Stop` before asserting disappearance.
- **New sidecar/log tests:** add failed-start and deadline cases and route a greater-than-64-KiB log line through `setupLogWriter`; the new large-frame test exercises `copyf` with a buffer, bypassing the server's scanner.
- **Preset environment additions:** assert RabbitMQ user override behavior and both Splunk acceptance variables for true/false license settings; successful default startup does not cover those option boundaries.
- **Router migration:** add assertions for the documented 405 headers/body, 307 cleanup redirect, encoded slash/dot segments, and concurrent requests returning independent preset instances.

## 4. Questions for the author

- What is the intended Kafka contract for custom tags such as `v3.9.0-L0` or `3-L0`, and for a Kafka 3 custom image with no explicit preset version? `preset/kafka/preset.go:214` recognizes only an integer before the first dot and otherwise silently omits ZooKeeper. The tested numeric tags are correct; the code cannot infer the server major from an arbitrary image alias. Should callers be required to supply a numeric version or explicit named ports, or should unrecognized tags be rejected/documented?
- Must pull-stream compatibility include daemons/proxies that emit only `{"error":"..."}` without `errorDetail`? The pinned Moby `Wait` reads `jsonstream.Message.Error`, whose JSON key is `errorDetail`; the new tests always supply both. Standard error-detail responses are handled correctly, but the broader claim about arbitrary `error` entries is not established.
- Should `WithAuthToken("")` explicitly suppress the host token, or intentionally use the fallback? Current behavior treats empty and absent identically. That is consistent with the current implementation's non-empty-token rule, but the public wording “when it is not set” does not settle explicit-empty intent.

## 5. Checked and fine

- **Agreed `WithOptions` semantics:** all 16 exported fields are represented and tested. New booleans are OR-ed, empty new slices/strings do not clear earlier values, and mounts merge into a separate destination map. The inability to turn these booleans off is an agreed decision, not a finding. Legacy `Debug`/name overwrite and non-nil empty port-map behavior remain intact.
- **Normal image pulls:** Moby `ImagePullResponse.Wait` consumes JSON, returns typed `errorDetail` failures, closes the response, and installs cancellation-driven closure. The fake-daemon tests meaningfully distinguish stream errors from HTTP errors and cover malformed JSON.
- **Daemon errors:** duplicate-name handling requires a typed conflict, extracts the known ID, and uses the agreed name fallback. Removal-in-progress handling retains both the conflict check and message qualification. I checked the short-ID concern against [Moby's removal implementation](https://github.com/moby/moby/blob/v28.0.0/daemon/delete.go#L28-L38): that implementation formats the request name/ID, so I did not report an unsupported short-versus-full-ID mismatch.
- **Port/network conversion:** the extracted gateway helper preserves the original behavior, skips nil/invalid endpoints, and has order-independent multiple-network assertions. TCP/UDP conversion, fixed host ports, Docker-environment binding, empty bindings, and malformed host-port handling have substantive tests. These paths were already using Moby types at the merge base; this range primarily bumps the SDK versions.
- **Mongo v2:** the loader still uses the BSON extended-JSON reader rather than lossy generic JSON conversion, closes each opened file, and passes the init context to inserts. The fixture checks a number above JavaScript's exact integer range, ObjectID, nested data, and equivalent canonical/relaxed dates. The pinned driver's ordinary disconnect path closes monitoring/pool resources even with a canceled context; I found no basis to claim a leak merely from `Disconnect(ctx)`.
- **MySQL/MariaDB concurrency:** both presets now use the same `sync.Once` before opening their own driver connections, eliminating the separate-once write/write race. Registry lookups construct fresh preset objects, and the restored parallel registry test exercises both presets.
- **Driver/default changes:** the architecture-specific MySQL files are replaced by one common image method; the Apache Cassandra import is used consistently; Elastic v7 client/test removal matches the decision. No additional defect was found in those adaptations.
- **Preset environment behavior:** Postgres conditionally emits `TZ`, and existing tests check both explicit and default timezone. Splunk sets General Terms only when license acceptance is enabled. RabbitMQ places its default Erlang arguments before caller options, so the caller replaces the value rather than merging flags; this matches the notes and [runc's ordered environment assignment](https://github.com/opencontainers/runc/blob/v1.2.6/libcontainer/init_linux.go#L226-L247). LocalStack explicit non-empty tokens take precedence over the host fallback, and the host token is not stored back into the preset.
- **HTTP security boundary:** there is no application-level image allowlist, mount-path restriction, or privilege authorization; values flow to structured Docker API fields, not a host shell. The new server documentation explicitly states that HTTP access grants Docker-host control. I have not treated that agreed capability as a new requirement to add authentication; the credential logging issue is separate.
- **Test/build infrastructure:** the image-default tests exercise constructors and JSON-created zero-value presets, the registry test avoids importing presets itself, and new contract tests are included in Taskfile targets. Dockerfiles use BuildKit target architecture variables and the cleaner build uses the workspace. The documented Mongo/mssql skips and dependency/CodeQL holds were not reopened, and no additional unexplained architecture skip was found in the committed diff.
