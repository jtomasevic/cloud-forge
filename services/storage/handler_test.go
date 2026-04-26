package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jtomasevic/cloud-forge/services/storage"
	"github.com/jtomasevic/cloud-forge/services/storage/generated"
)

// TestNewHandler verifies that NewHandler returns a non-nil, properly
// initialised Handler value that implements generated.StrictServerInterface.
func TestNewHandler(t *testing.T) {
	t.Parallel()

	h := storage.NewHandler()

	require.NotNil(t, h, "NewHandler must return a non-nil value")
	// Compile-time assertion: *Handler satisfies the interface.
	var _ generated.StrictServerInterface = h
}

// TestListBuckets_ReturnsNotImplemented verifies that the placeholder
// ListBuckets method returns a 500 response carrying the INTERNALERROR code.
// Real business logic will replace this in Phase 1.
func TestListBuckets_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	h := storage.NewHandler()
	resp, err := h.ListBuckets(
		context.Background(),
		generated.ListBucketsRequestObject{Tenant: "acme", Project: "demo"},
	)

	require.NoError(t, err, "placeholder must not propagate an error")
	// The concrete type must be ListBuckets500JSONResponse.
	got, ok := resp.(generated.ListBuckets500JSONResponse)
	require.True(t, ok, "expected ListBuckets500JSONResponse, got %T", resp)
	assert.Equal(t, generated.INTERNALERROR, got.Error.Code,
		"error code must be INTERNALERROR until Phase 1 is implemented")
	assert.NotEmpty(t, got.Error.Message, "error message must not be empty")
}

// TestCreateBucket_ReturnsNotImplemented verifies that the placeholder
// CreateBucket method returns a 500 response carrying the INTERNALERROR code.
func TestCreateBucket_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	h := storage.NewHandler()
	bodyName := "my-bucket"
	resp, err := h.CreateBucket(
		context.Background(),
		generated.CreateBucketRequestObject{
			Tenant:  "acme",
			Project: "demo",
			Body:    &generated.CreateBucketRequest{Name: bodyName},
		},
	)

	require.NoError(t, err)
	got, ok := resp.(generated.CreateBucket500JSONResponse)
	require.True(t, ok, "expected CreateBucket500JSONResponse, got %T", resp)
	assert.Equal(t, generated.INTERNALERROR, got.Error.Code)
	assert.NotEmpty(t, got.Error.Message)
}

// TestGetBucket_ReturnsNotImplemented verifies that the placeholder GetBucket
// method returns a 500 response carrying the INTERNALERROR code.
func TestGetBucket_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	h := storage.NewHandler()
	resp, err := h.GetBucket(
		context.Background(),
		generated.GetBucketRequestObject{Tenant: "acme", Project: "demo", Name: "alpha"},
	)

	require.NoError(t, err)
	got, ok := resp.(generated.GetBucket500JSONResponse)
	require.True(t, ok, "expected GetBucket500JSONResponse, got %T", resp)
	assert.Equal(t, generated.INTERNALERROR, got.Error.Code)
	assert.NotEmpty(t, got.Error.Message)
}

// TestDeleteBucket_ReturnsNotImplemented verifies that the placeholder
// DeleteBucket method returns a 500 response carrying the INTERNALERROR code.
func TestDeleteBucket_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	h := storage.NewHandler()
	resp, err := h.DeleteBucket(
		context.Background(),
		generated.DeleteBucketRequestObject{Tenant: "acme", Project: "demo", Name: "alpha"},
	)

	require.NoError(t, err)
	got, ok := resp.(generated.DeleteBucket500JSONResponse)
	require.True(t, ok, "expected DeleteBucket500JSONResponse, got %T", resp)
	assert.Equal(t, generated.INTERNALERROR, got.Error.Code)
	assert.NotEmpty(t, got.Error.Message)
}
