package main

import (
	"errors"
	"fmt"
	"log"
	"time"

	nats "github.com/nats-io/nats.go"
)

// JetStream producer that ensures a stream exists and publishes persistent messages.
func main() {
	// Hardcoded configuration for convenience.
	const (
		url     = "nats://127.0.0.1:4222"
		stream  = "PERSIST"
		subject = "persist.updates"
		count   = 5
		delay   = 500 * time.Millisecond
	)

	nc, err := nats.Connect(url)
	if err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer nc.Drain()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("jetstream ctx failed: %v", err)
	}

	// Ensure stream exists (idempotent).
	if err := ensureStream(js, stream, subject); err != nil {
		log.Fatalf("ensure stream failed: %v", err)
	}

	for i := 1; i <= count; i++ {
		payload := []byte(fmt.Sprintf("msg-%d @ %s", i, time.Now().Format(time.RFC3339Nano)))
		if _, err := js.Publish(subject, payload); err != nil {
			log.Fatalf("publish %d failed: %v", i, err)
		}
		log.Printf("published %d to %s: %s", i, subject, payload)
		time.Sleep(delay)
	}

	log.Println("producer done")
}

func ensureStream(js nats.JetStreamContext, name, subject string) error {
	// If the stream already exists, keep it.
	if _, err := js.StreamInfo(name); err == nil {
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return err
	}

	// Otherwise create it.
	_, err := js.AddStream(&nats.StreamConfig{
		Name:     name,
		Subjects: []string{subject},
		Storage:  nats.FileStorage,
	})
	return err
}
