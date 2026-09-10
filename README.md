# go-pop3

A performance-focused fork of [migadu/go-pop3](https://github.com/migadu/go-pop3),
maintained as the independent Go module `github.com/Jabberwocky238/go-pop3`.
Use `main` for this fork; `pr` contains only the performance patch intended for upstream
in [PR #3](https://github.com/migadu/go-pop3/pull/3).

## Changes in this fork

The POP3 body writer batches unchanged spans across ordinary CRLF lines instead
of issuing a write for every byte. It stops at bare LF or line-leading dots so
CRLF normalization and dot-stuffing still apply across fragmented writes.
Writer-owned scratch storage replaces escaping single-byte slices for inserted
bytes, and partial/short downstream writes are propagated. Message bodies remain
streamed with bounded buffers; no whole-message buffering is introduced.

On an Apple M4, macOS arm64, Go 1.25.3, the isolated 2 GiB writer benchmark improved
from **16.840 s to 0.248 s** (three-run medians), approximately **67.8× throughput**.
Allocations fell from approximately **2.15 billion/op to 6/op**, and allocated
memory from approximately **2 GiB/op to 4,248 B/op**.

| Isolated writer | Run 1 (s) | Run 2 (s) | Run 3 (s) | Median (s) |
| --- | ---: | ---: | ---: | ---: |
| Upstream `8794dc8d9e68` | 16.839746 | 16.708265 | 16.887655 | 16.839746 |
| Performance patch `cbaefcdb4447` | 0.260682 | 0.247973 | 0.248280 | 0.248280 |

### Additional ARM64 acceleration

The follow-up validator checks 16 bytes at a time with ARM64 NEON, preserving
bounded input reads and the scalar handling of bare LF, leading dots and errors.
An exceptional block disables further vector scans within that Write; newline-free
spans retain the standard-library search. Other architectures use the portable
bulk scanner. No new buffer, heap allocation, dependency or exported API is added.

On the same M4 / Go 1.25.3, a sequential matched-build comparison (three samples,
five 2 GiB operations each) measured **0.267945 s with `-tags=pop3scalar` versus
0.131334 s with the vector path: another 2.04x throughput**. Both medians remain
**4,248 B/op and 6 allocs/op**. Relative to the historical upstream 16.839746 s
median, this is approximately **128x on this ARM64 host**; that is not a claim
about other CPUs or end-to-end downloads.

`BenchmarkDotStuffWriterShapes` in the same test file additionally covers ordinary
CRLF, bare LF, leading dots and newline-free bodies. The latter three shapes
remain approximately unchanged after the adaptive fallback was added.

```sh
go test -tags=pop3scalar ./pop3server -run '^$' -bench '^BenchmarkDotStuffWriter($|Shapes)' -benchtime=5x -count=3
go test ./pop3server -run '^$' -bench '^BenchmarkDotStuffWriter($|Shapes)' -benchtime=5x -count=3
```

`pop3scalar` changes only the POP3 scanner. `purego` is also supported, but it can
turn off acceleration in TLS and other dependencies, so it is not an appropriate
end-to-end A/B switch. Tests include every byte value around vector and Write
boundaries, fragmented-input fuzzing, race tests and a Linux amd64 cross-build.

The benchmark is [BenchmarkDotStuffWriter in pop3server/dotstuff_bulk_test.go](pop3server/dotstuff_bulk_test.go).
It streams exactly 2 GiB from a repeated block of 76 ASCII `x` bytes plus CRLF,
through a 128 KiB copy buffer and the body writer into a default 4 KiB buffered
discard sink. Fixture and copy-buffer allocation are outside the timer; writer
construction, copying, Close and Flush are timed. The same benchmark was run
sequentially on both revisions without CPU profiling or race instrumentation.

```sh
go test ./pop3server -run '^$' -bench '^BenchmarkDotStuffWriter$' -benchtime=1x -count=3
```

This measures body transformation only, excluding sockets, TLS, storage, MIME
parsing and Base64 conversion. It checks byte count and errors, while separate
unit/fuzz tests cover protocol content and boundaries. Three samples do not
establish a confidence interval or a production SLA.

In a separate embedding-server test using native local Fals3y, POP3 RETR of a
2 GiB decoded attachment (2,938,662,361 MIME bytes) improved from **30.077 s to
1.790 s** after the writer fix. Those were single end-to-end runs with streamed
TLS downloads and content-hash verification, not the 67.8× isolated benchmark.

The performance commit passed unit/race tests, fragmented-write fuzzing, and the
embedding server's integration suite. This fork retains the upstream MIT license
and attribution. Public packages are now imported through this fork's module path.

A dependency-free POP3 server library for Go, with a matching minimal client
for proxy front-ends. You implement a single `Session` interface for storage and
authentication; the library handles the wire protocol, the RFC 1939 state
machine, dot-stuffing, TLS/STLS, timeouts, and abuse limits.

- Zero dependencies beyond the standard library.
- Streaming `RETR`/`TOP` — bodies are dot-stuffed and CRLF-normalised on the
  fly, never buffered whole.
- Hardened defaults — idle/absolute/write timeouts, line-length caps, a
  per-connection error limit with progressive back-off, panic isolation, and
  CRLF-injection-safe response encoding.
- Extensions — SASL PLAIN, STLS, CAPA, `LANG`/`UTF8`, custom capabilities, and a
  hook for unknown commands (e.g. Dovecot `XCLIENT`).
- Proxy-friendly — `Conn.Hijack()` and the `pop3client` package let a session
  authenticate upstream and relay raw bytes.


### POP3 resources with the updated storage binary

All rows transfer the same 2 GiB decoded size / 2,938,662,361 MIME bytes.
The first four runs use regenerated low-compressibility fixtures and were run
sequentially in scalar/vector/scalar/vector order; the last uses compressible data.
The POP3-only `pop3scalar` tag leaves TLS and storage dependencies unchanged.
All five complete runs passed their hashes, protocol checks and 256 MiB server
RSS ceiling. The scalar/vector sources differ only by the scanner build tag.

| Run | RETR wall (s) | Server CPU (s) | Average CPU (one core = 100%) | Initial RSS (MiB) | Peak RSS (MiB) | Sampled increase (KiB) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| scalar-1 | 2.055 | 1.780 | 86.60% | 131.109 | 131.141 | 32 |
| vector-1 | 1.849 | 1.560 | 84.36% | 71.359 | 71.719 | 368 |
| scalar-2 | 1.866 | 1.660 | 88.95% | 37.109 | 37.547 | 448 |
| vector-2 | 2.269 | 1.770 | 78.02% | 56.703 | 57.031 | 336 |
| compressible | 2.205 | 2.280 | 103.38% | 103.297 | 103.312 | 16 |

**These end-to-end results do not establish a download speedup.** The second
vector run is slower than its scalar counterpart. The isolated 2.04x encoding
gain remains valid, but does not remove network/storage work or guarantee lower
whole-process CPU in every run. Two samples per variant are insufficient for a
statistical regression claim. RSS includes memory retained by earlier phases;
the large differences in starting RSS cannot be attributed to this scanner.
No new buffer is allocated by the vector implementation.
CPU is summed user+system time across all server threads; RSS is sampled every
50 ms. The separate Fals3y and benchmark-client processes are excluded.

### Historical v0.1.5 CPU and memory

A follow-up single POP3S RETR using this fork's **v0.1.5** downloaded a 2 GiB decoded
attachment (2,938,662,361 MIME bytes) in **1.862 s**, using **1.65 CPU seconds**
for the embedding server process: **88.62% of one logical core on average**.
Sampled peak RSS was **139.875 MiB**, starting from
139.828 MiB, a **48 KiB** sampled increase during RETR.
The run passed content-hash checks and the application's 256 MiB RSS ceiling.

Measured on Apple M4, macOS arm64, Go 1.25.3, native local Fals3y, without profiling
or race instrumentation. RETR includes S3/gzip reads, dot-stuffing and TLS output;
connection setup, TLS handshake and login happened before timing. CPU is summed
across all server threads; 100% means one core, not the whole machine.

RSS was sampled every 50 ms and includes the complete embedding process, runtime,
TLS and storage buffers, including memory retained by earlier phases. It is not
the library's exclusive heap size; short peaks may be missed. RSS growth is not
allocation traffic. The 4,248 B/op microbenchmark result above measures allocations,
not RSS. Concurrent RETR resource usage has not been measured.

For the same 2 GiB attachment size, the earlier pre-fix run used **32.96 CPU seconds**
in 30.077 s (109.59% of one logical core). The first fixed run used **1.58 CPU seconds**
in 1.790 s (88.27%); the new direct-module run used **1.65 CPU seconds** in 1.862 s
(88.62%). Thus the fix uses about **95% less CPU time** than the historical pre-fix
record. Average utilization differs by only 0.35 percentage points between the
two fixed runs. These are single historical samples, not a repeated controlled
A/B experiment; the 4.4% CPU-time difference does not establish a regression.
Process/GC history and other application changes also prevent attributing all RSS
differences to the writer. CPU time per decoded GiB is 16.48 s before the fix,
0.79 s in the first fixed run, and 0.825 s in the new run.

See [upstream PR #3](https://github.com/migadu/go-pop3/pull/3) for raw counters,
resource analysis, methodology and the embedding harness command. The standalone
writer benchmark remains in `pop3server/dotstuff_bulk_test.go`.

## Install

```sh
go get github.com/Jabberwocky238/go-pop3@v0.1.6
```

Requires Go 1.25 or newer.

## Packages

| Package | Purpose |
| --- | --- |
| [`pop3`](./pop3) | Shared wire types (`MessageInfo`, `MessageUidl`, `Capability`). |
| [`pop3server`](./pop3server) | The server: `Server`, `Options`, the `Session` interface, `Conn`, `Error`. |
| [`pop3client`](./pop3client) | Minimal client for proxy front-ends. |
| [`pop3mem`](./pop3mem) | Concurrency-safe in-memory maildrop implementing `Session`; for tests and local dev. |

## Quick start

```go
package main

import (
	"log"

	"github.com/Jabberwocky238/go-pop3/pop3mem"
	"github.com/Jabberwocky238/go-pop3/pop3server"
)

func main() {
	store := pop3mem.New()
	store.AddUser("alice", "s3cret")
	store.AddMessage("alice", "uid-0001",
		[]byte("Subject: hello\r\n\r\nHi there.\r\n"))

	srv := pop3server.New(pop3server.Options{
		NewSession:   store.NewSession,
		Greeting:     "example.com POP3 ready",
		InsecureAuth: true, // allow USER/PASS without TLS — development only
	})

	log.Fatal(srv.ListenAndServe(":110"))
}
```

## Implementing a Session

`Options.NewSession` creates one `Session` per connection; the library calls
each method only in the correct protocol state.

```go
type Session interface {
	Close() error

	// AUTHORIZATION state
	Login(ctx context.Context, username, password string) error

	// TRANSACTION state (only after a successful Login)
	Stat(ctx context.Context) (count int, size int64, err error)
	List(ctx context.Context, msg int) ([]pop3.MessageInfo, error)
	Uidl(ctx context.Context, msg int) ([]pop3.MessageUidl, error)
	Retr(ctx context.Context, msg int) (io.ReadCloser, error)
	Top(ctx context.Context, msg, lines int) (io.ReadCloser, error)
	Dele(ctx context.Context, msg int) error
	Rset(ctx context.Context) error
	Noop(ctx context.Context) error
	Quit(ctx context.Context) (expunged int, err error)
}
```

Key contracts:

- The context carries a per-command deadline when `CommandTimeout` is set;
  propagate it into blocking work so operations abort on disconnect or timeout.
- Deletion is deferred: `Dele` marks, `Quit` commits. If the client drops
  without `QUIT`, `Close` runs without a preceding `Quit`, so do not commit
  pending deletions in `Close`.
- `Retr`/`Top` return an `io.ReadCloser` that the library streams (dot-stuffed,
  CRLF-normalised) and closes. Store messages CRLF-delimited so `Stat`/`List`
  octet counts match the wire form.

`pop3mem/store.go` is a complete reference implementation. Implement
`SessionSASL`, `SessionLang`, or `SessionUTF8` alongside `Session` to have the
library advertise and handle those extensions.

## Error handling

```go
// Plain error: text sent verbatim (unless StrictSessionErrors is set).
return errors.New("mailbox locked")

// *Error: explicit RFC 2449 response code, no internal string leak.
return &pop3server.Error{Code: "AUTH", Message: "authentication failed"}

// Respond, then close the connection.
return &pop3server.Error{Code: "SYS/TEMP", Message: "try later", Close: true}
```

Errors a session returns (e.g. "no such message") do not count toward
`MaxErrors` — the library cannot distinguish a client fault from a transient
backend failure. Errors the library detects (bad syntax, invalid arguments,
wrong-order or unknown commands, failed auth) do count.

## TLS

```go
srv := pop3server.New(pop3server.Options{
	NewSession: store.NewSession,
	TLSConfig:  &tls.Config{Certificates: []tls.Certificate{cert}},
	// InsecureAuth left false: auth is refused (and unadvertised) until secured.
})

go srv.ListenAndServeTLS(":995") // implicit TLS
go srv.ListenAndServe(":110")    // STLS upgrade advertised via CAPA
```

`ListenAndServe` does not wrap the listener in TLS even when `TLSConfig` is set;
use `ListenAndServeTLS` for implicit-TLS ports, or wrap your own listener with
`tls.NewListener` and call `Serve`.

## Configuration

Defaults are applied; every field is optional.

| Option | Default | Purpose |
| --- | --- | --- |
| `IdleTimeout` | 10m | Max wait for the next command. |
| `AuthIdleTimeout` | 0 (uses `IdleTimeout`) | Shorter idle limit while unauthenticated. |
| `AbsoluteSessionTimeout` | 0 (off) | Hard cap on total session duration. |
| `CommandTimeout` | 0 (off) | Per-command deadline via context. |
| `WriteTimeout` | 60s | Per-write deadline; bounds slow-reader stalls. |
| `MaxLineLength` | 1024 | Command-line length cap. |
| `MaxErrors` | 10 | Client protocol errors before disconnect (`-1` disables). |
| `ErrorDelay` / `MaxErrorDelay` | 0 / 30s | Progressive, interruptible back-off after each error. |
| `InsecureAuth` | false | Permit auth on plaintext connections. |
| `StrictSessionErrors` | false | Replace plain session-error text with a generic message. |

Hooks: `UnknownCommandHandler`, `OnCommand` (verb only, never credentials),
`OnPanic`, and a structured `Logger` (`slog`).

Connection concurrency is not capped internally. For a hard limit, wrap the
listener with `golang.org/x/net/netutil.LimitListener`; for a graceful
rejection, return an `*Error` from `NewSession`.

## Proxy front-ends

A session may authenticate upstream and take over the raw connection instead of
entering the TRANSACTION state. Call `Conn.Hijack()` from `Login` /
`AuthenticatePlain` to obtain the `net.Conn` and buffered reader (preserving
pipelined bytes), dial the backend with `pop3client`, and relay both directions.
See `pop3client/proxy_test.go` for a complete example.

## Standards

Implements POP3 (RFC 1939) with the extension mechanism of RFC 2449 (`CAPA`,
`RESP-CODES`, `PIPELINING`), `STLS` (RFC 2595), SASL `AUTH`/PLAIN (RFC 5034 /
RFC 4616), and `LANG`/`UTF8` (RFC 6856). The obsolete `LAST` command is rejected
without penalty for legacy-probing clients.

## Testing

```sh
go test ./...
go test -race ./...
```

## License

MIT — see [LICENSE](./LICENSE). Copyright (c) 2026 Migadu-Mail GmbH.
