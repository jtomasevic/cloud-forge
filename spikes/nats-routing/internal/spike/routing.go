package spike

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// RoutingStreamName is the JetStream stream name used by [RunContentBasedRouting].
const RoutingStreamName = "ROUTING_TEST"

// RoutingSubject is the broad subject that all CloudEvents are published to
// during the content-based routing test.  The subscriber reads the 'type'
// field and dispatches to per-type handlers.
const RoutingSubject = "events.all"

// defaultDispatchTimeout is the maximum time [RunContentBasedRouting] waits
// for all messages to be dispatched.  Tests override this via
// [RunContentBasedRoutingWithTimeout].
const defaultDispatchTimeout = 5 * time.Second

// RunContentBasedRouting demonstrates a Go-side dispatcher that reads the
// CloudEvents 'type' field from a NATS message payload and routes to one of
// the handlers registered in the provided routes map.
//
// Recommended approach for CF-EventRouter:
//   - Publish all events to a single broad JetStream subject ("events.all").
//   - The consumer decodes the envelope and branches on the 'type' field.
//   - No NATS-level content filtering is needed — new event types are added
//     by registering a new Go handler, not by changing the stream topology.
//
// The routes map defaults to [NewDefaultRoutes] when nil is passed.
// Pass a custom map to inject test doubles or override handlers.
func RunContentBasedRouting(
	ctx context.Context,
	nc *nats.Conn,
	routes map[string]RouteHandler,
	logger *slog.Logger,
) (pass bool, detail string) {
	return RunContentBasedRoutingWithTimeout(ctx, nc, routes, defaultDispatchTimeout, logger)
}

// RunContentBasedRoutingWithTimeout is the timeout-parameterised variant used
// by tests to avoid a hard 5s wait when verifying the timeout code path.
func RunContentBasedRoutingWithTimeout(
	ctx context.Context,
	nc *nats.Conn,
	routes map[string]RouteHandler,
	dispatchTimeout time.Duration,
	logger *slog.Logger,
) (pass bool, detail string) {
	if routes == nil {
		routes = NewDefaultRoutes()
	}

	// Create a JetStream context for persistent, ordered delivery.
	js, err := jetstream.New(nc)
	if err != nil {
		return false, fmt.Sprintf("jetstream.New failed: %v", err)
	}

	// Create (or reuse) a stream that captures all events on "events.>".
	// Memory storage keeps the spike fast on a laptop.
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     RoutingStreamName,
		Subjects: []string{"events.>"},
		Storage:  jetstream.MemoryStorage,
		Replicas: 1,
		// WorkQueue ensures each message is delivered to exactly one consumer.
		Retention: jetstream.WorkQueuePolicy,
	})
	if err != nil {
		return false, fmt.Sprintf("CreateOrUpdateStream failed: %v", err)
	}
	defer js.DeleteStream(ctx, RoutingStreamName) //nolint:errcheck

	// Build two test events — one created and one deleted — with different types
	// so both dispatch branches are exercised.
	events := []CloudEvent{
		BuildCloudEvent("com.cloudforge.bucket.created", "storage-svc", `{"name":"my-bucket"}`),
		BuildCloudEvent("com.cloudforge.bucket.deleted", "storage-svc", `{"name":"my-bucket"}`),
	}

	// Publish both events to the same subject.
	for _, ev := range events {
		// json.Marshal on CloudEvent (strings + json.RawMessage) never fails.
		data, _ := json.Marshal(ev)
		if _, err := js.Publish(ctx, RoutingSubject, data); err != nil {
			return false, fmt.Sprintf("js.Publish failed: %v", err)
		}
	}
	logger.Info("published CloudEvents", "count", len(events), "subject", RoutingSubject)

	// Create an ephemeral consumer — no Name/Durable, sufficient for a spike.
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		FilterSubject: "events.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return false, fmt.Sprintf("CreateOrUpdateConsumer failed: %v", err)
	}

	msgs, err := cons.Messages()
	if err != nil {
		return false, fmt.Sprintf("consumer.Messages() failed: %v", err)
	}
	defer msgs.Stop()

	dispatchErrors := ConsumeAndDispatch(ctx, msgs, routes, len(events), dispatchTimeout, logger)

	if len(dispatchErrors) > 0 {
		return false, "dispatch errors: " + strings.Join(dispatchErrors, "; ")
	}

	return true, "content-based routing works: dispatcher reads 'type' field and calls per-type handlers"
}

// ConsumeAndDispatch reads exactly n messages from msgs and dispatches each
// one using [Dispatch].  It collects and returns any dispatch errors.
//
// This function is exported so it can be unit-tested independently of the
// stream/consumer setup in [RunContentBasedRoutingWithTimeout].
func ConsumeAndDispatch(
	ctx context.Context,
	msgs jetstream.MessagesContext,
	routes map[string]RouteHandler,
	n int,
	timeout time.Duration,
	logger *slog.Logger,
) []string {
	var wg sync.WaitGroup
	wg.Add(n)

	var (
		dispatchErrors []string
		mu             sync.Mutex
	)

	go func() {
		for range n {
			msg, err := msgs.Next()
			if err != nil {
				mu.Lock()
				dispatchErrors = append(dispatchErrors, "msgs.Next: "+err.Error())
				mu.Unlock()
				wg.Done()
				continue
			}

			// Dispatch — Dispatch logs a warning for unknown types.
			result, err := Dispatch(msg.Data(), routes, logger)
			switch result {
			case DispatchOK:
				_ = msg.Ack()
			case DispatchUnknownType:
				// Discard unknown types in the spike; Phase 5 will send to DLQ.
				logger.Warn("discarding unrouted event", "error", err)
				_ = msg.Term()
			case DispatchDecodeError:
				mu.Lock()
				dispatchErrors = append(dispatchErrors, err.Error())
				mu.Unlock()
				_ = msg.Nak()
			}
			wg.Done()
		}
	}()

	// Wait for all messages to be dispatched or for timeout/context cancel.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(timeout):
		mu.Lock()
		dispatchErrors = append(dispatchErrors, "timeout waiting for messages")
		mu.Unlock()
	case <-ctx.Done():
		mu.Lock()
		dispatchErrors = append(dispatchErrors, "context cancelled: "+ctx.Err().Error())
		mu.Unlock()
	}

	mu.Lock()
	defer mu.Unlock()
	return dispatchErrors
}
