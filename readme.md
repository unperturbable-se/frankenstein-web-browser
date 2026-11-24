# Frankenstein

A browser built from parts.

**Go** handles the network — DNS, TCP, TLS, HTTP/1.1, connection pooling, crawling.
**Servo** handles rendering — HTML, CSS, layout, painting.

The two talk over IPC. Neither knows what the other is doing.

---

## Run it

```bash
go build -o frankenstein .
./frankenstein
```

No dependencies outside the standard library.

---

## Structure

| File | Does |
|---|---|
| `net.go` | `parseURL`, `dnsResolver`, `createTCPConnection`, `startTLSHandshake`, `httpGET` |
| `crawler.go` | URL normalization, link extraction, `connOwner`, `hostPool`, `http1_1` |
| `main.go` | Entry point |

---

## Why hand-rolled HTTP?

Because the network layer is the point. `net/http` hides the framing, pooling, and TLS that this project exists to learn. It's the right tool for production software and the wrong tool for a learning artifact.

## Why IPC for Servo?

CGO puts Go and Rust in the same process. A `panic!` in Rust kills the Go side. Shared memory over IPC keeps them isolated — and Servo is already multi-process, so this fits its design.

---

MIT.
