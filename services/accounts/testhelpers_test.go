package accounts_test

import "context"

// newBgCtx returns a background context for use in handler unit tests.
func newBgCtx() context.Context {
	return context.Background()
}
