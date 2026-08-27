package main

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

// applyMu keeps a write command's effect on memory and its append to the AOF
// atomic, so the order in the log always matches the order writes were applied.
var applyMu sync.Mutex

func main() {
	fmt.Println("Listening on port :6379")

	// create a new server
	l, err := net.Listen("tcp", ":6379")
	if err != nil {
		fmt.Println("Error starting server:", err)
		return
	}

	// create a new AOF file or open existing one
	aof, err := NewAof("database.aof")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer aof.Close() // close AOF file once finished

	// replay the log so the in-memory state survives a restart
	err = aof.Read(func(value Value) {
		if len(value.array) == 0 {
			fmt.Println("Skipping empty command in AOF")
			return
		}

		command := strings.ToUpper(value.array[0].bulk)
		args := value.array[1:]

		handler, ok := Handlers[command]
		if !ok {
			fmt.Println("Invalid command: ", command)
			return
		}

		handler(args)
	})
	if err != nil {
		fmt.Println("Error reading AOF:", err)
		return
	}

	// listen for connections, one goroutine per client
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}

		go handleConn(conn, aof)
	}
}

func handleConn(conn net.Conn, aof *Aof) {
	defer conn.Close() // close connection once finished

	// one reader for the life of the connection: a fresh bufio.Reader per
	// command would throw away anything it had already read ahead
	resp := NewResp(conn)
	writer := NewWriter(conn)

	for {
		value, err := resp.Read()
		if err != nil {
			if err != io.EOF {
				fmt.Println("Error reading from client:", err)
			}
			return
		}

		if value.typ != "array" {
			fmt.Println("Invalid request, expected array")
			continue
		}

		if len(value.array) == 0 {
			fmt.Println("Invalid request, expected array length > 0")
			continue
		}

		command := strings.ToUpper(value.array[0].bulk)
		args := value.array[1:]

		handler, ok := Handlers[command]
		if !ok {
			fmt.Println("Invalid command: ", command)
			writer.Write(Value{typ: "error", str: "ERR unknown command '" + command + "'"})
			continue
		}

		var result Value

		if command == "SET" || command == "HSET" {
			// apply and log as one step, so concurrent writers can't have
			// their effects and their log entries interleave differently
			applyMu.Lock()
			result = handler(args)
			if result.typ != "error" {
				if err := aof.Write(value); err != nil {
					fmt.Println("Error writing to AOF:", err)
				}
			}
			applyMu.Unlock()
		} else {
			result = handler(args)
		}

		writer.Write(result)
	}
}
