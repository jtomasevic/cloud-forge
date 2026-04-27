package spike

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// RunIsolationTest verifies that a message published by ncB (tenant-b) is
// completely invisible to ncA (tenant-a).
//
// NATS accounts create completely separate subject namespaces: even if both
// accounts subscribe to an identical subject string, messages are silently
// dropped at the server rather than crossing the account boundary.
//
// The test also runs a sanity check within ncA's own account to confirm that
// the silence above was caused by isolation and not a broken subscription.
func RunIsolationTest(
	ctx context.Context,
	ncA, ncB *nats.Conn,
	subject string,
	logger *slog.Logger,
) (pass bool, detail string) {
	logger.Info("isolation test: subscribing ncA", "subject", subject)

	// Buffer of 1 is enough — we only care whether any message arrived at all.
	received := make(chan *nats.Msg, 1)

	// Subscribe via core NATS (not JetStream) — fastest path to verify subject
	// isolation without stream setup overhead.
	sub, err := ncA.Subscribe(subject, func(msg *nats.Msg) {
		received <- msg
	})
	if err != nil {
		return false, fmt.Sprintf("ncA subscribe failed: %v", err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	// Flush ensures the subscription is registered before we publish.
	if err := ncA.Flush(); err != nil {
		return false, fmt.Sprintf("ncA flush failed: %v", err)
	}

	// Publish the spy message from ncB.  Because ncB is authenticated into a
	// different NATS account, the server will never route this message to ncA's
	// subscription — even though the subject string is identical.
	if err := ncB.Publish(subject, []byte(`{"spy":"should-not-arrive"}`)); err != nil {
		return false, fmt.Sprintf("ncB publish failed: %v", err)
	}
	if err := ncB.Flush(); err != nil {
		return false, fmt.Sprintf("ncB flush failed: %v", err)
	}

	// 300ms is more than enough for a local loopback delivery if isolation
	// were broken.
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()

	select {
	case msg := <-received:
		// Isolation is broken — ncA received ncB's message.
		return false, fmt.Sprintf("ISOLATION BROKEN: ncA received ncB message: %s", msg.Data)

	case <-timer.C:
		// Expected path: no message arrived within the observation window.
		logger.Info("isolation confirmed: ncA received nothing from ncB after 300ms")
	}

	// ── Sanity check: ncA can still receive its own messages ───────────────
	// This rules out the possibility that the subscription itself is broken.
	sanitySubject := subject + ".sanity"
	sanityReceived := make(chan struct{}, 1)

	sanity, err := ncA.Subscribe(sanitySubject, func(_ *nats.Msg) {
		sanityReceived <- struct{}{}
	})
	if err != nil {
		return false, fmt.Sprintf("sanity subscribe failed: %v", err)
	}
	defer sanity.Unsubscribe() //nolint:errcheck

	if err := ncA.Publish(sanitySubject, []byte("ping")); err != nil {
		return false, fmt.Sprintf("sanity publish failed: %v", err)
	}

	sanityTimer := time.NewTimer(300 * time.Millisecond)
	defer sanityTimer.Stop()

	select {
	case <-sanityReceived:
		logger.Info("sanity check passed: ncA can receive its own messages")
	case <-sanityTimer.C:
		return false, "sanity check failed: ncA did not receive its own message within 300ms"
	case <-ctx.Done():
		return false, "context cancelled during sanity check"
	}

	return true, "NATS account isolation is complete: cross-account messages are silently dropped"
}
