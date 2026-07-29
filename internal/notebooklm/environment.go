package notebooklm

import (
	"os"
	"strings"
)

var providerEnvironmentNames = map[string]struct{}{
	"ALL_PROXY":          {},
	"APPDATA":            {},
	"COMSPEC":            {},
	"CURL_CA_BUNDLE":     {},
	"HOMEDRIVE":          {},
	"HOMEPATH":           {},
	"HOME":               {},
	"HTTPS_PROXY":        {},
	"HTTP_PROXY":         {},
	"LANG":               {},
	"LOCALAPPDATA":       {},
	"LOGNAME":            {},
	"NO_PROXY":           {},
	"PATH":               {},
	"PATHEXT":            {},
	"PROGRAMDATA":        {},
	"REQUESTS_CA_BUNDLE": {},
	"SHELL":              {},
	"SSL_CERT_DIR":       {},
	"SSL_CERT_FILE":      {},
	"SYSTEMROOT":         {},
	"TEMP":               {},
	"TERM":               {},
	"TMP":                {},
	"TMPDIR":             {},
	"TZ":                 {},
	"USER":               {},
	"USERPROFILE":        {},
	"WINDIR":             {},
	"XDG_CACHE_HOME":     {},
	"XDG_CONFIG_HOME":    {},
	"XDG_DATA_HOME":      {},
	"XDG_RUNTIME_DIR":    {},
	"XDG_STATE_HOME":     {},
}

// providerEnvironment exposes only the operating-system context needed to
// locate provider profiles and reach the network. Impartus credentials and
// unrelated application tokens must never cross the subprocess boundary.
func providerEnvironment() []string {
	return filterProviderEnvironment(os.Environ())
}

func filterProviderEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upperName := strings.ToUpper(name)
		if _, ok := providerEnvironmentNames[upperName]; ok || strings.HasPrefix(upperName, "LC_") {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
