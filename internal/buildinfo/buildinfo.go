package buildinfo

var (
	Version = "0.1.1" // x-release-please-version
	Date    = ""
	Commit  = "unknown"
)

func SentryRelease() string {
	return "impartus-cli@" + Version
}
