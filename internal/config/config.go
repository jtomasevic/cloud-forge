// Package config provides Viper-based configuration loading for all
// CloudForge services.
//
// # Loading order
//
// Configuration values are resolved in the following precedence order
// (later sources override earlier ones):
//
//  1. Default values embedded in the config struct.
//  2. YAML config file — path from CF_CONFIG_FILE env var, defaults to ./config.yaml.
//  3. Environment variables with the CF_ prefix, e.g. CF_SERVER_PORT maps to server.port.
//  4. Kubernetes-mounted secret files in the directory given by CF_SECRETS_DIR.
//     Each file in that directory is read as a key=value pair and merged into
//     the configuration map.
//
// # Validation
//
// After loading, the resulting struct is validated with the
// github.com/go-playground/validator/v10 struct tags. Any validation failure
// returns an error that describes which field failed and why.
//
// # Usage
//
//	type ServiceConfig struct {
//	    Server struct {
//	        Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
//	        Host string `mapstructure:"host" validate:"required"`
//	    } `mapstructure:"server"`
//	    OTLPEndpoint string `mapstructure:"otlp_endpoint"`
//	}
//
//	cfg := config.MustLoad(ServiceConfig{
//	    Server: struct{Port int; Host string}{Port: 8080, Host: "0.0.0.0"},
//	})
package config

// Loader is the interface implemented by the default Viper-backed loader.
// Services that need to inject a test double (e.g. a fixed in-memory config)
// can replace the loader by satisfying this interface.
type Loader[T any] interface {
	// Load reads all config sources, merges them in precedence order,
	// and populates a value of type T. It returns a validation error if any
	// required field is missing or fails a validate tag constraint.
	Load(defaults T) (T, error)
}

// Options controls optional loader behaviour. The zero value is valid
// and uses all defaults (CF_ prefix, ./config.yaml file path, no secrets dir).
type Options struct {
	// EnvPrefix is the uppercase environment variable prefix used when
	// mapping env vars to config keys (default: "CF").
	// e.g. with prefix "CF", the env var CF_SERVER_PORT maps to server.port.
	EnvPrefix string

	// ConfigFile overrides the default config file path (./config.yaml).
	// Ignored if empty.
	ConfigFile string

	// SecretsDir is the directory that contains Kubernetes-mounted secret files.
	// Each file is treated as a single config value; the filename is the key.
	// Ignored if empty.
	SecretsDir string
}
