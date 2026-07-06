package client

import (
	"encoding/json"
	"os"
)

// SecretLookup returns a getSecret hook for the reverse tunnel. It reads
// secretsPath once (a JSON object of {"KEY":"value"}); each requested key is
// resolved from that map first, then from the process environment. A missing or
// malformed file is treated as an empty map (environment-only), not an error.
func SecretLookup(secretsPath string) func(key string) string {
	fileSecrets := map[string]string{}
	if data, err := os.ReadFile(secretsPath); err == nil {
		_ = json.Unmarshal(data, &fileSecrets) // malformed → empty map, env-only
	}
	return func(key string) string {
		if v, ok := fileSecrets[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}
