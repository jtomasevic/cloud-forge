package bench

import (
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// TestHashAPIKey_Deterministic verifies that hashing the same input twice
// produces the same output (required for the CF-Router lookup to work
// correctly on subsequent requests with the same raw key).
func TestHashAPIKey_Deterministic(t *testing.T) {
	t.Parallel()
	key := []byte("test-api-key-for-benchmarking")
	h1 := HashAPIKey(key)
	h2 := HashAPIKey(key)
	if h1 != h2 {
		t.Errorf("non-deterministic: got %q then %q", h1, h2)
	}
}

// TestHashAPIKey_Length verifies the hash is a 64-character hex string
// (BLAKE2b-256 = 32 bytes × 2 hex chars = 64 chars).
func TestHashAPIKey_Length(t *testing.T) {
	t.Parallel()
	hash := HashAPIKey([]byte("any-key"))
	if len(hash) != 64 {
		t.Errorf("expected 64 hex chars, got %d: %s", len(hash), hash)
	}
}

// TestHashAPIKey_ValidHex verifies the output is valid hex (not base64 etc.).
func TestHashAPIKey_ValidHex(t *testing.T) {
	t.Parallel()
	hash := HashAPIKey([]byte("another-key"))
	if _, err := hex.DecodeString(hash); err != nil {
		t.Errorf("invalid hex output %q: %v", hash, err)
	}
}

// TestHashAPIKey_MatchesBlake2b256 cross-checks our wrapper against direct
// use of blake2b.New256 to ensure we are computing the correct hash algorithm.
func TestHashAPIKey_MatchesBlake2b256(t *testing.T) {
	t.Parallel()
	key := []byte("cf_live_benchmark_key_12345")

	// Compute directly.
	h, _ := blake2b.New256(nil)
	h.Write(key)
	want := hex.EncodeToString(h.Sum(nil))

	got := HashAPIKey(key)
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestHashAPIKey_DifferentInputsDifferentHashes verifies that two different
// keys produce different hashes (basic collision property check).
func TestHashAPIKey_DifferentInputsDifferentHashes(t *testing.T) {
	t.Parallel()
	h1 := HashAPIKey([]byte("key-one"))
	h2 := HashAPIKey([]byte("key-two"))
	if h1 == h2 {
		t.Error("different keys produced the same hash")
	}
}

// TestSplitStatements_CommentStripping verifies that SQL-style comment lines
// are removed before the CQL is split on semicolons.
func TestSplitStatements_CommentStripping(t *testing.T) {
	t.Parallel()
	src := `
-- This is a comment
CREATE KEYSPACE foo;
-- Another comment
CREATE TABLE foo.bar (id UUID PRIMARY KEY);
`
	stmts := splitStatements(src)
	for _, s := range stmts {
		if len(s) > 0 && (s[0] == '-' && len(s) > 1 && s[1] == '-') {
			t.Errorf("comment line leaked into statements: %q", s)
		}
	}
}

// TestSplitStatements_CountsStatements verifies the correct number of
// non-empty statements are returned for a simple multi-statement input.
func TestSplitStatements_CountsStatements(t *testing.T) {
	t.Parallel()
	src := "CREATE KEYSPACE cf;\nCREATE TABLE cf.t (id UUID PRIMARY KEY);"
	stmts := splitStatements(src)

	// Count non-empty statements.
	nonEmpty := 0
	for _, s := range stmts {
		if trimmed := trimWhitespace(s); trimmed != "" {
			nonEmpty++
		}
	}
	if nonEmpty != 2 {
		t.Errorf("expected 2 non-empty statements, got %d", nonEmpty)
	}
}

// TestRandomRawKey_Length verifies RandomRawKey returns exactly n bytes.
func TestRandomRawKey_Length(t *testing.T) {
	t.Parallel()
	for _, n := range []int{16, 32, 64} {
		key, err := RandomRawKey(n)
		if err != nil {
			t.Fatalf("RandomRawKey(%d): unexpected error: %v", n, err)
		}
		if len(key) != n {
			t.Errorf("RandomRawKey(%d): got %d bytes, want %d", n, len(key), n)
		}
	}
}

// TestRandomRawKey_Uniqueness verifies two calls return different values.
func TestRandomRawKey_Uniqueness(t *testing.T) {
	t.Parallel()
	a, _ := RandomRawKey(32)
	b, _ := RandomRawKey(32)
	if string(a) == string(b) {
		t.Error("two RandomRawKey calls returned identical bytes — CSPRNG failure")
	}
}

// trimWhitespace is a test helper that trims spaces and newlines from a string.
func trimWhitespace(s string) string {
	result := []byte{}
	for _, c := range []byte(s) {
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			result = append(result, c)
		}
	}
	return string(result)
}
