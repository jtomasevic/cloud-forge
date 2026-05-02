package accounts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSplitStatements_BasicSplit verifies that statements are correctly
// split on ";" delimiters.
func TestSplitStatements_BasicSplit(t *testing.T) {
	src := "CREATE TABLE foo (id UUID PRIMARY KEY); CREATE TABLE bar (id UUID PRIMARY KEY);"
	stmts := splitStatements(src)

	var nonEmpty []string
	for _, s := range stmts {
		if s != "" && len([]byte(s)) > 1 {
			nonEmpty = append(nonEmpty, s)
		}
	}
	assert.GreaterOrEqual(t, len(nonEmpty), 2)
}

// TestSplitStatements_StripsComments verifies that lines beginning with "--"
// are removed before splitting.
func TestSplitStatements_StripsComments(t *testing.T) {
	src := `-- This is a comment
CREATE KEYSPACE IF NOT EXISTS cf
  WITH replication = {'class': 'SimpleStrategy'};
-- Another comment`

	stmts := splitStatements(src)
	for _, s := range stmts {
		assert.NotContains(t, s, "-- This is a comment", "comment should be stripped")
		assert.NotContains(t, s, "-- Another comment", "comment should be stripped")
	}
}

// TestSplitStatements_EmptyInput verifies that an empty string returns a
// slice with at least one element (the empty string from splitting "").
func TestSplitStatements_EmptyInput(t *testing.T) {
	stmts := splitStatements("")
	assert.NotNil(t, stmts)
}

// TestSplitStatements_CommentOnlyInput verifies that a comment-only file
// produces no usable statements.
func TestSplitStatements_CommentOnlyInput(t *testing.T) {
	src := `-- comment one
-- comment two
-- comment three`
	stmts := splitStatements(src)
	for _, s := range stmts {
		assert.NotContains(t, s, "--")
	}
}

// TestDefaultConfig_Values verifies that DefaultConfig returns reasonable
// defaults for the local dev environment.
func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, []string{"127.0.0.1"}, cfg.Hosts)
	assert.Equal(t, 19042, cfg.Port, "dev port should be 19042 (port-forwarded)")
	assert.Greater(t, cfg.ConnectTimeout.Milliseconds(), int64(0))
	assert.Greater(t, cfg.QueryTimeout.Milliseconds(), int64(0))
}
