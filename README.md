# Redis From Scratch

A minimal Redis server written in Go from scratch — no libraries, just the standard library.

## Main idea

Redis speaks a simple text protocol (RESP) over TCP. This repo implements enough of it to be a real
Redis server: accept a TCP connection, parse RESP commands, run them against in-memory maps, append
every write to a log on disk, and serialize the reply back as RESP. Any standard client
(`redis-cli`) can talk to it.

The code is deliberately split by concern:

| File | Responsibility |
| --- | --- |
| `main.go` | TCP listener, AOF replay on boot, and the read → dispatch → write loop |
| `resp.go` | RESP protocol: `Value` type, reader (deserialize) and writer (`Marshal`) |
| `handler.go` | Command implementations and the `Handlers` command table |
| `aof.go` | Append-only file: durable log of every write, replayed at startup |

## How to run

```bash
go run .
```

The server listens on `:6379` and keeps its state in `database.aof` in the working directory.
Connect with the standard client:

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

Restart the server and the data is still there — `SET` and `HSET` are replayed from `database.aof`
at boot.

## Choices

- **Hand-rolled RESP.** `Value` is one struct covering every RESP type (`string`, `error`, `bulk`,
  `array`, `null`), with a `Marshal` switch per type. The reader only handles `*` (array) and `$`
  (bulk), because that is all a client ever sends — commands always arrive as an array of bulk strings.
- **Command table over a switch.** `Handlers` maps an uppercased command name to a
  `func([]Value) Value`. Adding a command is one map entry and one function; `main.go` never changes.
- **Two stores, two locks.** `SETs` and `HSETs` each get their own `sync.RWMutex` — reads take
  `RLock` so concurrent `GET`s don't block each other.
- **AOF, not snapshots.** Persistence is a log of the commands themselves, written as RESP. That
  means the replay path is the same parser the network path uses: `NewResp(file)` in a loop, feeding
  each command back through `Handlers`. No separate serialization format to maintain.
- **`fsync` once per second.** `file.Write` only reaches the OS page cache; a background goroutine
  calls `Sync()` every second to force it to the physical disk. A process crash loses nothing, and a
  power cut loses at most a second of writes — the same trade-off real Redis calls
  `appendfsync everysec`.
- **Only successful writes are logged.** A command is appended to the AOF after its handler runs and
  only if it didn't return an error, so a rejected command can't come back to life on the next boot.
- **A goroutine per connection.** `main` loops on `Accept` and hands each client to `handleConn`,
  which owns one `Resp` reader and one `Writer` for the life of that connection. Reusing the reader
  matters: a fresh `bufio.Reader` per command would discard bytes it had already buffered, silently
  dropping pipelined commands.
- **Apply and log under one lock.** `applyMu` wraps "run the handler, then append to the AOF" for
  write commands. The per-store `RWMutex`es already make each map access safe on its own, but without
  this the two steps could interleave between goroutines and leave the log in a different order than
  the writes were applied — so a replay would rebuild different data than the server held. Reads
  never take it and stay fully parallel.


## Source

  The idea behind the projects and most of the code comes from https://www.build-redis-from-scratch.dev/en/introduction
  