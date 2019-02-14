package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/nats-io/go-nats"
	"github.com/nats-io/go-nats-streaming"
	"github.com/nats-io/go-nats-streaming/pb"

	"github.com/uw-labs/proximo/proto"
)

type natsStreamingConsumeHandler struct {
	clusterID   string
	maxInflight int
	nc          *nats.Conn
}

func newNatsStreamingConsumeHandler(url, clusterID string, maxInflight int) (*natsStreamingConsumeHandler, error) {
	nc, err := nats.Connect(url, nats.Name("proximo-nats-streaming-"+generateID()))
	if err != nil {
		return nil, err
	}
	return &natsStreamingConsumeHandler{nc: nc, clusterID: clusterID, maxInflight: maxInflight}, nil
}

func (h *natsStreamingConsumeHandler) Close() error {
	h.nc.Close()
	return nil
}

func (h *natsStreamingConsumeHandler) HandleConsume(ctx context.Context, req *proto.StartConsumeRequest, forClient chan<- *proto.Message, confirmRequest <-chan *proto.Confirmation) error {

	conn, err := stan.Connect(h.clusterID, req.GetConsumer()+generateID(), stan.NatsConn(h.nc))
	if err != nil {
		return err
	}

	// there can be at most `h.maxInflight` unacknowledged messages at any time
	ackQueue := make(chan *stan.Msg, h.maxInflight)

	ackErrors := make(chan error)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case msg := <-ackQueue:
				select {
				case cr := <-confirmRequest:
					seq, err := strconv.ParseUint(cr.MsgID, 10, 64)
					if err != nil {
						ackErrors <- fmt.Errorf("failed to parse message sequence '%v'", cr.MsgID)
						return
					}
					if seq != msg.Sequence {
						ackErrors <- fmt.Errorf("unexpected message sequence. was %v but wanted %v.", seq, msg.Sequence)
						return
					}
					if err := msg.Ack(); err != nil {
						ackErrors <- fmt.Errorf("failed to ack message with NATS: %v.", err.Error())
						return
					}
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	f := func(msg *stan.Msg) {
		select {
		case <-ctx.Done():
			return
		case forClient <- &proto.Message{Data: msg.Data, Id: strconv.FormatUint(msg.Sequence, 10)}:
			ackQueue <- msg
		}
	}

	subscription, err := conn.QueueSubscribe(
		req.GetTopic(),
		req.GetConsumer(),
		f,
		stan.StartAt(pb.StartPosition_First),
		stan.DurableName(req.GetConsumer()),
		stan.SetManualAckMode(),
		stan.AckWait(60*time.Second),
		stan.MaxInflight(h.maxInflight),
	)
	if err != nil {
		return err
	}
	closeAll := func() error {
		var closeErr error
		for _, c := range []io.Closer{subscription, conn} {
			if err := c.Close(); err != nil {
				closeErr = multierror.Append(closeErr, err)
			}
		}
		return closeErr
	}

	select {
	case <-ctx.Done():
		wg.Wait()
		return closeAll()
	case err := <-ackErrors:
		wg.Wait()
		closeAll()
		return err
	}

}

type natsStreamingProduceHandler struct {
	clusterID   string
	maxInflight int
	nc          *nats.Conn
}

func newNatsStreamingProduceHandler(url, clusterID string, maxInflight int) (*natsStreamingProduceHandler, error) {
	nc, err := nats.Connect(url, nats.Name("proximo-nats-streaming-"+generateID()))
	if err != nil {
		return nil, err
	}
	return &natsStreamingProduceHandler{nc: nc, clusterID: clusterID, maxInflight: maxInflight}, nil
}

func (h *natsStreamingProduceHandler) Close() error {
	h.nc.Close()
	return nil
}

func (h *natsStreamingProduceHandler) HandleProduce(ctx context.Context, conf producerConfig, forClient chan<- *proto.Confirmation, messages <-chan *proto.Message) error {

	conn, err := stan.Connect(h.clusterID, generateID(), stan.NatsConn(h.nc))
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return conn.Close()
		case msg := <-messages:
			err := conn.Publish(conf.topic, msg.GetData())
			if err != nil {
				conn.Close()
				return err
			}
			select {
			case forClient <- &proto.Confirmation{MsgID: msg.GetId()}:
			case <-ctx.Done():
				return conn.Close()
			}
		}
	}

}
