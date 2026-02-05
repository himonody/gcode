package main

import (
    "bufio"
    "fmt"
    "log"
    "net"
    "os"
)

const defaultAddr = "localhost:9000"

func main() {
    addr := defaultAddr
    if len(os.Args) > 1 {
        addr = os.Args[1]
    }

    conn, err := net.Dial("tcp", addr)
    if err != nil {
        log.Fatalf("failed to connect to %s: %v", addr, err)
    }
    defer conn.Close()

    log.Printf("connected to %s", addr)

    scanner := bufio.NewScanner(os.Stdin)
    reader := bufio.NewReader(conn)

    fmt.Println("Type messages and press Enter to send. Ctrl+C to exit.")

    for {
        fmt.Print("> ")
        if !scanner.Scan() {
            log.Println("input closed")
            return
        }
        text := scanner.Text() + "\n"

        if _, err := conn.Write([]byte(text)); err != nil {
            log.Fatalf("write failed: %v", err)
        }

        resp, err := reader.ReadString('\n')
        if err != nil {
            log.Fatalf("read failed: %v", err)
        }
        fmt.Printf("server: %s", resp)
    }
}
