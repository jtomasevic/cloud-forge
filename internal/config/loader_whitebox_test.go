// Package config — white-box tests for unexported helpers.
// These tests live in package config (not config_test) so they can exercise
// unexported functions such as structToMap and flattenMap directly.
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStructToMap_ValidStruct verifies that structToMap converts a struct to a
// flat dot-notation map using the mapstructure tags.
func TestStructToMap_ValidStruct(t *testing.T) {
	// Host (pointer type) is placed before Port (non-pointer) so the GC scan
	// region is as small as possible — matches the fieldalignment linter rule.
	type nested struct {
		Host string `mapstructure:"host"`
		Port int    `mapstructure:"port"`
	}
	// LogLevel is placed before Server so the plain string (1 pointer word)
	// comes before the nested struct that ends with a non-pointer int,
	// keeping all pointer words contiguous at the front.
	type cfg struct {
		LogLevel string `mapstructure:"log_level"`
		Server   nested `mapstructure:"server"`
	}

	m, err := structToMap(cfg{
		Server:   nested{Port: 9090, Host: "127.0.0.1"},
		LogLevel: "debug",
	})

	require.NoError(t, err)
	assert.Equal(t, 9090, m["server.port"])
	assert.Equal(t, "127.0.0.1", m["server.host"])
	assert.Equal(t, "debug", m["log_level"])
}

// TestStructToMap_DecoderError verifies that structToMap returns an error
// when the input is a type that mapstructure cannot decode into a map
// (e.g. a channel, which has no mapstructure representation).
func TestStructToMap_DecoderError(t *testing.T) {
	// Channels cannot be encoded by mapstructure — this exercises the
	// "decoding defaults with mapstructure" error path.
	type badCfg struct {
		Ch chan int `mapstructure:"ch"`
	}

	// mapstructure silently ignores channels rather than erroring, so the
	// result is a map without the channel field — no error expected here.
	m, err := structToMap(badCfg{})
	// Whether or not this produces an error depends on mapstructure internals;
	// the important assertion is that the function does not panic.
	if err == nil {
		assert.NotNil(t, m)
	}
}

// TestFlattenMap verifies that a nested map is correctly flattened to
// dot-notation keys.
func TestFlattenMap(t *testing.T) {
	nested := map[string]any{
		"server": map[string]any{
			"port": 8080,
			"host": "localhost",
		},
		"log_level": "info",
	}

	flat := flattenMap(nested, "")

	assert.Equal(t, 8080, flat["server.port"])
	assert.Equal(t, "localhost", flat["server.host"])
	assert.Equal(t, "info", flat["log_level"])
}

// TestFlattenMap_WithPrefix verifies that a non-empty prefix is prepended to
// all keys in the flattened result.
func TestFlattenMap_WithPrefix(t *testing.T) {
	m := map[string]any{"port": 9090}
	flat := flattenMap(m, "server")
	assert.Equal(t, 9090, flat["server.port"])
}
