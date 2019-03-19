package main

import (
	"context"

	"github.com/Shopify/sarama"
	"github.com/uw-labs/proximo/proto"
	"github.com/uw-labs/substrate"
	"github.com/uw-labs/substrate/kafka"
)

type KafkaAsyncSourceFactory struct {
	brokers []string
	version *sarama.KafkaVersion
}

func (f KafkaAsyncSourceFactory) NewAsyncSource(ctx context.Context, conf consumerConfig) (substrate.AsyncMessageSource, error) {
	var offset int64
	switch conf.offset {
	case proto.Offset_OFFSET_OLDEST, proto.Offset_OFFSET_DEFAULT:
		offset = kafka.OffsetOldest
	case proto.Offset_OFFSET_NEWEST:
		offset = kafka.OffsetNewest
	}
	return kafka.NewAsyncMessageSource(kafka.AsyncMessageSourceConfig{
		ConsumerGroup: conf.consumer,
		Topic:         conf.topic,
		Offset:        offset,
		Brokers:       f.brokers,
		Version:       f.version,
	})
}

type KafkaAsyncSinkFactory struct {
	brokers []string
	version *sarama.KafkaVersion
}

func (f KafkaAsyncSinkFactory) NewAsyncSink(ctx context.Context, config SinkConfig) (substrate.AsyncMessageSink, error) {
	return kafka.NewAsyncMessageSink(kafka.AsyncMessageSinkConfig{
		Topic:   config.Topic,
		Brokers: f.brokers,
		Version: f.version,
	})
}
