// SPDX-License-Identifier: Apache-2.0

package secrets

import "strings"

// MasterKeyEnv holds the passphrase for the encrypted secrets file.
const MasterKeyEnv = "LIVESUBS_MASTER_KEY"

// GenericEnvPrefix makes any secret settable from the environment:
// secret "session:main:youtube_url" is read from
// LIVESUBS_SECRET_SESSION_MAIN_YOUTUBE_URL.
const GenericEnvPrefix = "LIVESUBS_SECRET_"

// envAliases are the well-known variables checked, in order, before the
// generic LIVESUBS_SECRET_<NAME> variable.
var envAliases = map[string][]string{
	"google_api_key":         {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"obs_websocket_password": {"OBS_WEBSOCKET_PASSWORD"},
}

// EnvVars returns the environment variables that provide secret name, in
// lookup order.
func EnvVars(name string) []string {
	vars := append([]string(nil), envAliases[name]...)
	return append(vars, GenericEnvPrefix+envSuffix(name))
}

// envSuffix upper-cases name and turns every other character into '_'.
func envSuffix(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, name)
}
