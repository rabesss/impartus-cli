package downloader

// outputPublication separates failures that prevented publication from a
// durability warning discovered only after the final path became visible.
type outputPublication struct {
	Warning error
}
