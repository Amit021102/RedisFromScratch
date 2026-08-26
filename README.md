# Redis From Scratch

A minimal Redis server written in Go from scratch — no libraries, just `net` and the standard library.

## Main idea

Redis speaks a simple text protocol (RESP) over TCP. This repo implements enough of it to be a real
Redis server: accept a TCP connection, parse RESP commands, run them against in-memory maps, and
serialize the reply back as RESP. Any standard client (`redis-cli`) can talk to it.

The code is deliberately split by concern:

| File | Responsibility |
| --- | --- |
| `main.go` | TCP listener and the read → dispatch → write loop |
| `resp.go` | RESP protocol: `Value` type, reader (deserialize) and writer (`Marshal`) |
| `handler.go` | Command implementations and the `Handlers` command table |
| `aof.go` | Placeholder for append-only-file persistence (not implemented yet) |

## How to run

There is no `go.mod` committed yet, so initialize the module once:

```bash
go mod init redisfromscratch   # first time only
go run .
```

The server listens on `:6379`. Connect with the standard client:

```bash
redis-cli -p 6379
```

## Supported workflows

Strings:

```
SET name amit
GET name          # -> "amit"
GET missing       # -> (nil)
```

Hashes:

```
HSET users u1 amit
HGET users u1     # -> "amit"
HGETALL users     # -> flat array of field/value pairs
```

Health check:

```
PING              # -> PONG
PING hello        # -> "hello"
```

Everything lives in memory and is lost on restart.

## Choices

- **Hand-rolled RESP.** `Value` is one struct covering every RESP type (`string`, `error`, `bulk`,
  `array`, `null`), with a `Marshal` switch per type. The reader only handles `*` (array) and `$`
  (bulk), because that is all a client ever sends — commands always arrive as an array of bulk strings.
- **Command table over a switch.** `Handlers` maps an uppercased command name to a
  `func([]Value) Value`. Adding a command is one map entry and one function; `main.go` never changes.
- **Two stores, two locks.** `SETs` and `HSETs` each get their own `sync.RWMutex` — reads take
  `RLock` so concurrent `GET`s don't block each other. The locks are already correct for the
  multi-client server the next step brings.
- **One client at a time.** `l.Accept()` is called once, outside the loop, so the server handles a
  single connection and exits when it closes. Deliberate for now: it keeps the request loop flat and
  easy to read. The fix is a `for { conn, _ := l.Accept(); go handle(conn) }`.
- **Persistence deferred.** `aof.go` is a stub. The plan is an append-only file: log every write
  command as RESP and replay it on boot — which the existing `Resp` reader already knows how to parse.
