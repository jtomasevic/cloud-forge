//go:build integration

package testutil

import (
	"context"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
)

// minioImage is the MinIO server image.
const minioImage = "minio/minio:RELEASE.2024-01-01T00-00-00Z"

// minioAccessKey and minioSecretKey are the fixed credentials used for test
// containers. These must never appear in production configuration.
const (
	minioAccessKey = "minio-test-access"
	minioSecretKey = "minio-test-secret"
)

// StartMinIO starts a MinIO container and returns a connected *minio.Client.
//
// The client is pre-configured with the test credentials and points to the
// container's mapped API port. It is safe to create buckets and upload objects
// immediately after this function returns — the container is fully ready.
//
// The cleanup function terminates the container and is also registered with
// t.Cleanup as a safety net.
func StartMinIO(t *testing.T) (*minio.Client, func()) {
	t.Helper()

	ctx := context.Background()

	// Start the MinIO container using the testcontainers-go MinIO module.
	container, err := tcminio.Run(ctx,
		minioImage,
		tcminio.WithUsername(minioAccessKey),
		tcminio.WithPassword(minioSecretKey),
	)
	if err != nil {
		t.Fatalf("testutil: starting minio container: %v", err)
	}

	// Resolve the HTTP endpoint that Docker has mapped for the MinIO API port.
	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: getting minio endpoint: %v", err)
	}

	// Create an anonymous MinIO client. We disable SSL because the test
	// container does not have TLS configured — that would add unnecessary
	// complexity to the test setup.
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: creating minio client: %v", err)
	}

	// Verify connectivity with a lightweight list-buckets request.
	if _, err := client.ListBuckets(ctx); err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("testutil: pinging minio: %v", err)
	}

	cleanup := func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("testutil: warning: failed to terminate minio container: %v", err)
		}
	}

	t.Cleanup(cleanup)

	return client, cleanup
}
