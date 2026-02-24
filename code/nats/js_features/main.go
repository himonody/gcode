package main

import (
	"errors"
	"log"
	"time"

	nats "github.com/nats-io/nats.go"
)

// This example configures JetStream for durability, ordering, deduplication, delay/scheduled delivery, and DLQ via republish.
// It sets everything up with hardcoded values and demonstrates publish patterns.
func main() {
	const (
		url       = "nats://127.0.0.1:4222"
		stream    = "PERSIST"
		subject   = "persist.updates"
		durable   = "PERSIST_DURABLE"
		dlqSubj   = "DLQ.persist"
		deliverTo = "deliver.persist"
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

	// 1) Durable, persisted stream with dedup window for at-least-once and no-loss.
	if err := ensureStream(js, stream, subject); err != nil {
		log.Fatalf("ensure stream failed: %v", err)
	}

	// 2) Consumer for ordered delivery (push, auto-restart on gaps).
	if _, err := js.Subscribe(subject, func(m *nats.Msg) {
		log.Printf("ordered recv seq=%d data=%s", m.Meta().Sequence.Consumer, string(m.Data))
		m.Ack()
	}, nats.OrderedConsumer()); err != nil {
		log.Fatalf("ordered subscribe failed: %v", err)
	}

	// 3) Durable pull consumer with DLQ (republish) and max redeliveries.
	if err := ensureDlqConsumer(js, stream, subject, durable, deliverTo, dlqSubj); err != nil {
		log.Fatalf("ensure DLQ consumer failed: %v", err)
	}
	// Delivery subject subscription.
	if _, err := nc.Subscribe(deliverTo, func(m *nats.Msg) {
		log.Printf("worker got: %s", string(m.Data))
		// Simulate failure on specific payload to trigger DLQ republish.
		if string(m.Data) == "fail" {
			m.Nak()
			return
		}
		m.Ack()
	}); err != nil {
		log.Fatalf("delivery sub failed: %v", err)
	}
	// DLQ listener.
	if _, err := nc.Subscribe(dlqSubj, func(m *nats.Msg) {
		log.Printf("DLQ received: %s", string(m.Data))
	}); err != nil {
		log.Fatalf("dlq sub failed: %v", err)
	}

	// 4) Publish deduplicated messages (Msg-Id header) to avoid duplicates.
	publishOnce(js, subject, "dedup-1", []byte("once"))
	publishOnce(js, subject, "dedup-1", []byte("once")) // skipped by dedup window

	// 5) Publish normal + failure message to show DLQ after retries.
	js.Publish(subject, []byte("ok"))
	js.Publish(subject, []byte("fail"))

	// 6) Delay and scheduled messages.
	publishWithDelay(js, subject, 5*time.Second, []byte("delayed-5s"))
	publishNotBefore(js, subject, time.Now().Add(10*time.Second), []byte("not-before-10s"))

	log.Println("setup complete; waiting to process messages (press Ctrl+C to exit)")
	select {}
}

func ensureStream(js nats.JetStreamContext, name, subject string) error {
	if _, err := js.StreamInfo(name); err == nil {
		return nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return err
	}

	_, err := js.AddStream(&nats.StreamConfig{
		Name:       name,
		Subjects:   []string{subject},
		Storage:    nats.FileStorage,
		Retention:  nats.LimitsPolicy,
		Discard:    nats.DiscardOld,
		Duplicates: 5 * time.Minute, // enable dedup within window via Msg-Id
	})
	return err
}

func ensureDlqConsumer(js nats.JetStreamContext, stream, subject, durable, deliverTo, dlqSubj string) error {
	if _, err := js.ConsumerInfo(stream, durable); err == nil {
		return nil
	} else if !errors.Is(err, nats.ErrConsumerNotFound) {
		return err
	}

	// Durable pull consumer with max redeliveries and DLQ republish.
	_, err := js.AddConsumer(stream, &nats.ConsumerConfig{
		Durable:        durable,
		AckPolicy:      nats.AckExplicitPolicy,
		FilterSubject:  subject,
		DeliverSubject: deliverTo,
		MaxDeliver:     3,
		BackOff:        []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second},
		Republish: &nats.Republish{
			Source:      subject,
			Destination: dlqSubj,
			HeadersOnly: false,
		},
	})
	return err
}

func publishOnce(js nats.JetStreamContext, subject, msgID string, data []byte) {
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	msg.Header.Set(nats.MsgIdHdr, msgID)
	if _, err := js.PublishMsg(msg); err != nil {
		log.Printf("publishOnce failed: %v", err)
	}
}

func publishWithDelay(js nats.JetStreamContext, subject string, delay time.Duration, data []byte) {
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	msg.Header.Set(nats.MsgDelay, delay.String())
	if _, err := js.PublishMsg(msg); err != nil {
		log.Printf("publishWithDelay failed: %v", err)
	}
}

func publishNotBefore(js nats.JetStreamContext, subject string, at time.Time, data []byte) {
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	msg.Header.Set(nats.MsgNotBefore, at.UTC().Format(time.RFC3339Nano))
	if _, err := js.PublishMsg(msg); err != nil {
		log.Printf("publishNotBefore failed: %v", err)
	}
}
