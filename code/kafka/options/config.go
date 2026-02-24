// Package options 提供 Kafka 的基础配置和生产环境优化建议
package options

import (
	"crypto/tls"
	"crypto/x509"
	"log"
	"os"
	"time"

	"github.com/IBM/sarama"
)

// init 桥接 Sarama 的内部日志到标准日志。
// 在生产环境中，开启它可以帮助通过日志排查网络抖动、协议不匹配或连接断开等底层问题。
func init() {
	sarama.Logger = log.New(os.Stdout, "[Sarama] ", log.LstdFlags)
}

// Config 包装了自定义连接参数，方便在不同环境（开发/生产）间切换。
type Config struct {
	Brokers  []string // Kafka 代理节点列表
	SASL     bool     // 是否启用 SASL 认证
	User     string   // SASL 用户名
	Password string   // SASL 密码
	TLS      bool     // 是否启用 TLS 加密
	CAFile   string   // CA 根证书路径
	CertFile string   // 客户端证书路径
	KeyFile  string   // 客户端私钥路径
}

// GetDefaultConfig 返回基础的 Sarama 配置，并根据生产环境最佳实践进行了优化。
func GetDefaultConfig() *sarama.Config {
	config := sarama.NewConfig()
	// 显式指定版本极其重要，确保客户端与 Broker 之间的协议匹配（V3.0.0+ 支持更多特性，如更强的幂等性）。
	config.Version = sarama.V3_0_0_0

	// 网络超时配置：避免在极端网络情况下长时间阻塞
	config.Net.DialTimeout = 10 * time.Second
	config.Net.ReadTimeout = 10 * time.Second
	config.Net.WriteTimeout = 10 * time.Second
	config.Net.KeepAlive = 30 * time.Second

	// 元数据刷新频率：自动感知 Topic 增加或 Partition 变动
	config.Metadata.Retry.Max = 3
	config.Metadata.RefreshFrequency = 5 * time.Minute

	return config
}

// SetupSASLConfig 配置 SASL 认证。
// 生产环境集群通常开启 SASL_PLAINTEXT 或 SASL_SSL。
func SetupSASLConfig(config *sarama.Config, user, password string) {
	config.Net.SASL.Enable = true
	config.Net.SASL.User = user
	config.Net.SASL.Password = password
	config.Net.SASL.Handshake = true
	config.Net.SASL.Mechanism = sarama.SASLTypePlaintext
}

// SetupTLSConfig 配置 TLS 认证。
func SetupTLSConfig(config *sarama.Config, caFile, certFile, keyFile string) error {
	t := &tls.Config{
		InsecureSkipVerify: false,
	}

	if caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		t.RootCAs = caCertPool
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return err
		}
		t.Certificates = []tls.Certificate{cert}
	}

	config.Net.TLS.Enable = true
	config.Net.TLS.Config = t
	return nil
}

// GetProducerConfig 获取生产者专用配置。
// reliable 为 true 时开启“不丢消息”模式：acks=-1，开启幂等性，多重试。
func GetProducerConfig(reliable bool) *sarama.Config {
	config := GetDefaultConfig()
	config.Producer.Return.Successes = true // 同步/异步生产者都需要根据此值判断发送结果
	config.Producer.Return.Errors = true

	// 开启 Snappy 压缩：在 CPU 消耗和网络带宽之间取得良好平衡，适合生产环境的高吞吐量。
	config.Producer.Compression = sarama.CompressionSnappy

	if reliable {
		// acks = -1：确保所有副本都收到消息。
		config.Producer.RequiredAcks = sarama.WaitForAll
		// 开启幂等性：解决网络波动导致的重试可能产生的消息重复。
		config.Producer.Idempotent = true
		// 现代 Kafka (V1.1+) 支持在开启幂等性时设置 MaxOpenRequests > 1，V3.0.0 推荐设置。
		config.Net.MaxOpenRequests = 5
		// 增加重试次数，应对瞬时故障。
		config.Producer.Retry.Max = 10
		config.Producer.Retry.Backoff = 100 * time.Millisecond
	}

	return config
}

// GetConsumerConfig 获取消费者组配置。
func GetConsumerConfig(groupID string) *sarama.Config {
	config := GetDefaultConfig()
	// 如果消费者组是新的，从最早的消息开始消费，防止漏消费。
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	config.Consumer.Return.Errors = true

	// 负载均衡策略：Sticky 策略能在重平衡时尽量保持原有分配关系，减少震荡。
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.NewBalanceStrategySticky(),
		sarama.NewBalanceStrategyRange(),
	}

	// 生产环境关键超时参数：
	// SessionTimeout：Broker 多久没收到心跳认为消费者掉线。
	config.Consumer.Group.Session.Timeout = 20 * time.Second
	// HeartbeatInterval：心跳频率，建议设为 SessionTimeout 的 1/3。
	config.Consumer.Group.Heartbeat.Interval = 6 * time.Second
	// MaxWaitTime：Fetch 请求在 Broker 端的最长阻塞等待时间。
	config.Consumer.MaxWaitTime = 500 * time.Millisecond
	// MaxProcessingTime：处理单次批次消息的最长时间，超过此值会触发重平衡。
	config.Consumer.MaxProcessingTime = 100 * time.Millisecond

	return config
}
