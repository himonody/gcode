package main

import (
    "bufio"
    "fmt"
    "log"
    "net"
    "os"
    "os/signal"
    "strings"
    "syscall"
)

const defaultAddr = ":9000"

func main() {
    addr := defaultAddr
    if envAddr := os.Getenv("TCP_ADDR"); envAddr != "" {
        addr = envAddr
    }

    ln, err := net.Listen("tcp", addr)
    if err != nil {
        log.Fatalf("failed to listen on %s: %v", addr, err)
    }
    defer ln.Close()

    log.Printf("TCP server listening on %s", addr)

    // Graceful shutdown on SIGINT/SIGTERM
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        <-stop
        log.Println("shutting down listener...")
        ln.Close()
    }()

    for {
        conn, err := ln.Accept()
        if err != nil {
            // If the listener is closed, exit the loop for clean shutdown.
            if opErr, ok := err.(*net.OpError); ok && !opErr.Temporary() {
                log.Printf("listener closed: %v", err)
                break
            }
            log.Printf("accept error: %v", err)
            continue
        }
        go handleConn(conn)
    }
}

func handleConn(conn net.Conn) {
    defer conn.Close()

    remote := conn.RemoteAddr().String()
    log.Printf("accepted connection from %s", remote)

    reader := bufio.NewReader(conn)
    writer := bufio.NewWriter(conn)

    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            log.Printf("connection %s closed: %v", remote, err)
            return
        }
        cleaned := strings.TrimRight(line, "\r\n")
        log.Printf("received from %s: %s", remote, cleaned)

        response := fmt.Sprintf("echo: %s\n", cleaned)
        if _, err := writer.WriteString(response); err != nil {
            log.Printf("write error to %s: %v", remote, err)
            return
        }
        if err := writer.Flush(); err != nil {
            log.Printf("flush error to %s: %v", remote, err)
            return
        }
    }
}
