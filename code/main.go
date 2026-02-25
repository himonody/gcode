package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"strings"
	"syscall"
	"time"

	"github.com/IBM/sarama"
)

// Configure your Kafka brokers here or via env BROKERS (comma-separated).
var brokers = []string{"localhost:9092"}

func main() {
	a := 6.3
	b := 6.3
	fmt.Println(a == b)
	//if env := os.Getenv("BROKERS"); env != "" {
	//	brokers = strings.Split(env, ",")
	//}
	//
	//if len(os.Args) < 3 {
	//	fmt.Println("Usage:")
	//	fmt.Println("  go run main.go produce <topic> <message>")
	//	fmt.Println("  go run main.go consume <topic>")
	//	fmt.Println("  go run main.go create-topic <topic> [partitions] [replicationFactor]")
	//	return
	//}
	//
	//cmd := os.Args[1]
	//topic := os.Args[2]
	//
	//switch cmd {
	//case "produce":
	//	if len(os.Args) < 4 {
	//		log.Fatalf("produce requires a message argument")
	//	}
	//	message := strings.Join(os.Args[3:], " ")
	//	if err := produceMessage(topic, message); err != nil {
	//		log.Fatalf("produce failed: %v", err)
	//	}
	//case "consume":
	//	if err := consumeTopic(topic); err != nil {
	//		log.Fatalf("consume failed: %v", err)
	//	}
	//case "create-topic":
	//	partitions := 1
	//	replication := int16(1)
	//	if len(os.Args) >= 4 {
	//		if p, err := strconv.Atoi(os.Args[3]); err == nil && p > 0 {
	//			partitions = p
	//		} else {
	//			log.Fatalf("invalid partitions: %v", os.Args[3])
	//		}
	//	}
	//	if len(os.Args) >= 5 {
	//		if r, err := strconv.Atoi(os.Args[4]); err == nil && r > 0 {
	//			replication = int16(r)
	//		} else {
	//			log.Fatalf("invalid replicationFactor: %v", os.Args[4])
	//		}
	//	}
	//	if err := createTopic(topic, partitions, replication); err != nil {
	//		log.Fatalf("create-topic failed: %v", err)
	//	}
	//default:
	//	log.Fatalf("unknown command %q", cmd)
	//}
}

func newProducer() (sarama.SyncProducer, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 5
	cfg.Producer.Idempotent = true
	cfg.Net.MaxOpenRequests = 1 // required for idempotent producer
	cfg.Version = sarama.V2_5_0_0

	return sarama.NewSyncProducer(brokers, cfg)
}

func produceMessage(topic, message string) error {
	producer, err := newProducer()
	if err != nil {
		return err
	}
	defer producer.Close()

	partition, offset, err := producer.SendMessage(&sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(fmt.Sprintf("key-%d", time.Now().UnixNano())),
		Value: sarama.StringEncoder(message),
	})
	if err != nil {
		return err
	}

	log.Printf("sent message to %s partition=%d offset=%d", topic, partition, offset)
	return nil
}

func createTopic(topic string, partitions int, replication int16) error {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_5_0_0

	admin, err := sarama.NewClusterAdmin(brokers, cfg)
	if err != nil {
		return err
	}
	defer admin.Close()

	err = admin.CreateTopic(topic, &sarama.TopicDetail{
		NumPartitions:     int32(partitions),
		ReplicationFactor: replication,
	}, false)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	log.Printf("topic %s ensured with partitions=%d replication=%d", topic, partitions, replication)
	return nil
}

func consumeTopic(topic string) error {
	groupID := fmt.Sprintf("sarama-sample-%d", time.Now().UnixNano())

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_5_0_0
	cfg.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest

	group, err := sarama.NewConsumerGroup(brokers, groupID, cfg)
	if err != nil {
		return err
	}
	defer group.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := consumerGroupHandler{}

	// Handle interrupts to exit cleanly.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signals
		cancel()
	}()

	for {
		if err := group.Consume(ctx, []string{topic}, handler); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

type consumerGroupHandler struct{}

func (consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }
func (consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		log.Printf("received message topic=%s partition=%d offset=%d key=%s value=%s",
			msg.Topic, msg.Partition, msg.Offset, string(msg.Key), string(msg.Value))
		session.MarkMessage(msg, "")
	}
	return nil
}
