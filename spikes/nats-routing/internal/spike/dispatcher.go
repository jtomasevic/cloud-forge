package spike

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// DispatchResult classifies the outcome of a single Dispatch call.
type DispatchResult int

const (
	// DispatchOK means the event was decoded and a handler was called.
	DispatchOK DispatchResult = iota
	// DispatchUnknownType means no handler was registered for the event type.
	DispatchUnknownType
	// DispatchDecodeError means the raw bytes could not be decoded as a
	// CloudEvent JSON envelope.
	DispatchDecodeError
)

// Dispatch decodes a CloudEvent from raw bytes and calls the matching handler
// from the routes map.
//
// This is a pure function: it requires no NATS connection and can be tested
// without any infrastructure.  The routing loop in [RunContentBasedRouting]
// calls Dispatch for every JetStream message it receives.
//
// Dispatch returns:
//   - (DispatchOK, nil) when the handler was called.
//   - (DispatchUnknownType, err) when no route is registered for the type.
//   - (DispatchDecodeError, err) when the payload cannot be unmarshalled.
func Dispatch(
	data []byte,
	routes map[string]RouteHandler,
	logger *slog.Logger,
) (DispatchResult, error) {
	// Decode the CloudEvent envelope.
	var ev CloudEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return DispatchDecodeError, fmt.Errorf("unmarshal CloudEvent: %w", err)
	}

	// Look up the registered handler for this event type.
	handler, ok := routes[ev.Type]
	if !ok {
		// Log at Warn so operators can detect unrouted events without failing.
		logger.Warn("no route registered for event type", "type", ev.Type)
		return DispatchUnknownType, fmt.Errorf("no route for type %q", ev.Type)
	}

	// Invoke the handler.  Handler errors are intentionally not propagated —
	// the caller (the JetStream consume loop) decides whether to Ack or Nak.
	handler(ev, logger)
	return DispatchOK, nil
}

// NewDefaultRoutes returns the default CloudForge event type → handler map
// used by [RunContentBasedRouting].
func NewDefaultRoutes() map[string]RouteHandler {
	return map[string]RouteHandler{
		"com.cloudforge.bucket.created": HandleBucketCreated,
		"com.cloudforge.bucket.deleted": HandleBucketDeleted,
	}
}

// HandleBucketCreated handles "com.cloudforge.bucket.created" events.
// In production this would enqueue a 	 MakeBucket call.
func HandleBucketCreated(ev CloudEvent, logger interface{ Info(string, ...any) }) {
	logger.Info("→ handleBucketCreated",
		"id", ev.ID,
		"source", ev.Source,
		"data", string(ev.Data),
	)
}

// HandleBucketDeleted handles "com.cloudforge.bucket.deleted" events.
// In production this would enqueue a MinIO RemoveBucket call.
func HandleBucketDeleted(ev CloudEvent, logger interface{ Info(string, ...any) }) {
	logger.Info("→ handleBucketDeleted",
		"id", ev.ID,
		"source", ev.Source,
		"data", string(ev.Data),
	)
}
