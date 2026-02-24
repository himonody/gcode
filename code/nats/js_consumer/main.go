package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	nats "github.com/nats-io/nats.go"
)

// JetStream consumer that binds to a durable and pulls messages with ack.
func main() {
	// Hardcoded configuration for convenience.
	const (
		url     = "nats://127.0.0.1:4222"
		stream  = "PERSIST"
		subject = "persist.updates"
		durable = "PERSIST_DURABLE"
		batch   = 5
		wait    = 2 * time.Second
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

	// Ensure consumer exists/bind; if absent, create durable pull consumer.
	sub, err := js.PullSubscribe(subject, durable, nats.BindStream(stream))
	if err != nil {
		// Try creating the durable if it doesn't exist.
		_, cErr := js.AddConsumer(stream, &nats.ConsumerConfig{
			Durable:       durable,
			AckPolicy:     nats.AckExplicitPolicy,
			FilterSubject: subject,
		})
		if cErr != nil {
			log.Fatalf("add consumer failed: %v", cErr)
		}
		sub, err = js.PullSubscribe(subject, durable, nats.BindStream(stream))
		if err != nil {
			log.Fatalf("pull subscribe failed: %v", err)
		}
	}

	log.Printf("pulling from stream=%s subj=%s durable=%s", stream, subject, durable)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case s := <-sigCh:
			log.Printf("signal: %v", s)
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), wait)
		msgs, err := sub.Fetch(batch, nats.Context(ctx))
		cancel()
		if err != nil {
			if err == context.DeadlineExceeded {
				continue // no messages within wait
			}
			log.Fatalf("fetch failed: %v", err)
		}

		for _, msg := range msgs {
			log.Printf("[%s] %s", msg.Subject, string(msg.Data))
			msg.Ack()
		}
	}
}
