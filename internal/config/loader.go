package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
)

// defaultEnvPrefix is used when Options.EnvPrefix is empty.
const defaultEnvPrefix = "CF"

// validate is a package-level validator instance reused across all Load calls.
// Reusing the instance is important for performance because it caches struct
// reflection metadata on first use.
var validate = validator.New()

// Load populates a value of type T from all configuration sources in
// precedence order (defaults → YAML file → env vars → secret files).
//
// T must be a struct annotated with `mapstructure` tags matching the
// config key hierarchy and optional `validate` tags for field constraints.
//
// The defaults parameter serves two purposes: it sets fallback values AND
// defines the shape of the config struct that Viper will unmarshal into.
func Load[T any](defaults T, opts ...Options) (T, error) {
	// Merge caller options with defaults.
	o := resolveOptions(opts)

	v := viper.New()

	// ── Step 1: Encode default values into Viper ──────────────────────────
	// Convert the defaults struct to a map via JSON so nested structs are
	// handled correctly, then register each leaf key as a Viper default.
	defaultMap, err := structToMap(defaults)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("config: encoding defaults: %w", err)
	}
	for key, val := range defaultMap {
		v.SetDefault(key, val)
	}

	// ── Step 2: YAML config file ──────────────────────────────────────────
	configFile := o.ConfigFile
	if configFile == "" {
		// Prefer the CF_CONFIG_FILE environment variable, then fall back to
		// the conventional ./config.yaml path.
		if envFile := os.Getenv("CF_CONFIG_FILE"); envFile != "" {
			configFile = envFile
		} else {
			configFile = "config.yaml"
		}
	}

	v.SetConfigFile(configFile)

	// ReadInConfig is only fatal if the file exists but cannot be parsed.
	// A missing file is fine — services run with env vars and defaults.
	if err := v.ReadInConfig(); err != nil {
		//nolint:gosec // G703: configFile is either an explicit caller-supplied path or
		// the hard-coded default "config.yaml". Taint analysis is a false positive here;
		// the value is not derived from untrusted user input.
		if _, statErr := os.Stat(configFile); statErr == nil {
			// File exists but could not be read — that IS an error.
			var zero T
			return zero, fmt.Errorf("config: reading config file %q: %w", configFile, err)
		}
		// File simply does not exist — proceed without it.
	}

	// ── Step 3: Environment variables ────────────────────────────────────
	prefix := o.EnvPrefix
	if prefix == "" {
		prefix = defaultEnvPrefix
	}

	v.SetEnvPrefix(prefix)
	v.AutomaticEnv()

	// Replace dots and hyphens in config key names with underscores so that
	// nested keys (e.g. "server.port") map cleanly to CF_SERVER_PORT.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))

	// ── Step 4: Kubernetes secret files ──────────────────────────────────
	secretsDir := o.SecretsDir
	if secretsDir == "" {
		secretsDir = os.Getenv("CF_SECRETS_DIR")
	}

	if secretsDir != "" {
		if err := loadSecretFiles(v, secretsDir); err != nil {
			var zero T
			return zero, fmt.Errorf("config: loading secrets from %q: %w", secretsDir, err)
		}
	}

	// ── Step 5: Unmarshal into T ──────────────────────────────────────────
	var cfg T
	if err := v.Unmarshal(&cfg); err != nil {
		var zero T
		return zero, fmt.Errorf("config: unmarshalling config: %w", err)
	}

	// ── Step 6: Validate ──────────────────────────────────────────────────
	if err := validate.Struct(cfg); err != nil {
		var zero T
		return zero, fmt.Errorf("config: validation failed: %w", err)
	}

	return cfg, nil
}

// MustLoad is like Load but panics on error.
// Intended for use in main() where a misconfigured service should not start.
func MustLoad[T any](defaults T, opts ...Options) T {
	cfg, err := Load(defaults, opts...)
	if err != nil {
		panic(fmt.Sprintf("cloudforge: failed to load configuration: %v", err))
	}
	return cfg
}

// resolveOptions merges the caller-supplied options with zero-value defaults.
// Only the first Options value is used; subsequent values are ignored.
func resolveOptions(opts []Options) Options {
	if len(opts) == 0 {
		return Options{}
	}
	return opts[0]
}

// structToMap converts a struct to a flat dot-notation map[string]any using
// the mapstructure tag names (which Viper also uses for Unmarshal). This
// ensures that nested keys like server.port are correctly registered as
// Viper defaults with the right key path.
//
// For example, a struct field tagged `mapstructure:"server"` containing a
// nested struct with `mapstructure:"port"` produces the key "server.port".
func structToMap(v any) (map[string]any, error) {
	// First, convert the struct to a nested map[string]any using the
	// mapstructure tags — this is the same tag Viper uses for Unmarshal.
	var nested map[string]any
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:  &nested,
		TagName: "mapstructure",
	})
	if err != nil {
		return nil, fmt.Errorf("creating mapstructure decoder: %w", err)
	}
	if err := dec.Decode(v); err != nil {
		return nil, fmt.Errorf("decoding defaults with mapstructure: %w", err)
	}

	// Flatten the nested map to dot-notation keys so they can be registered
	// with v.SetDefault(key, value).
	return flattenMap(nested, ""), nil
}

// flattenMap recursively flattens a nested map into a flat map with
// dot-separated key paths.
//
// Example:
//
//	{"server": {"port": 8080}} → {"server.port": 8080}
func flattenMap(m map[string]any, prefix string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		// Build the fully-qualified key path.
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		// If the value is a nested map, recurse; otherwise store it directly.
		if nested, ok := v.(map[string]any); ok {
			for nk, nv := range flattenMap(nested, key) {
				result[nk] = nv
			}
		} else {
			result[key] = v
		}
	}
	return result
}

// loadSecretFiles reads each regular file in dir and sets the file's base name
// (lowercased) as a Viper key with the trimmed file content as the value.
//
// For example, a file named "DATABASE_PASSWORD" containing "s3cr3t\n" sets
// the config key "database_password" to "s3cr3t". Files beginning with "."
// and subdirectories are silently ignored.
func loadSecretFiles(v *viper.Viper, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading secrets directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		//nolint:gosec // G304: path is constructed from a trusted secrets directory (dir)
		// and a filename read from os.ReadDir — not from user-controlled HTTP input.
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading secret file %q: %w", entry.Name(), err)
		}

		// Trim surrounding whitespace and newlines that Kubernetes appends
		// when mounting secrets as files.
		value := strings.TrimSpace(string(content))
		key := strings.ToLower(entry.Name())
		v.Set(key, value)
	}

	return nil
}
