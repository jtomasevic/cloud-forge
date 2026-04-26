// Package testutil provides testcontainer helpers for CloudForge integration tests.
//
// # Overview
//
// Each helper starts a real Docker container, waits for it to be ready, and
// returns a pre-configured client along with a cleanup function. The cleanup
// function stops and removes the container when the test completes.
//
// # Build tag
//
// All files in this package carry the `//go:build integration` build tag.
// Tests importing this package must also carry the tag so they are excluded
// from the CI unit-test job (`go test -short ./...`) and only run in the
// dedicated integration-test job.
//
// # Pattern
//
// Every helper follows the same contract:
//
//	func StartXxx(t *testing.T) (client, func()) {
//	    t.Helper()
//	    // start container with testcontainers-go
//	    // wait for readiness
//	    // return (client, cleanup)
//	}
//
// The cleanup function is also registered with t.Cleanup so the container
// is stopped even if the caller forgets to call it.
//
// # Usage
//
//	//go:build integration
//
//	func TestMyFeature(t *testing.T) {
//	    pool, cleanup := testutil.StartPostgres(t)
//	    defer cleanup()
//	    // use pool ...
//	}
package testutil
