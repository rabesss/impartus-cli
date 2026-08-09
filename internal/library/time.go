package library

import "time"

// Fixed-width UTC timestamps preserve chronological ordering in SQLite TEXT
// comparisons, including events within the same second.
const databaseTimeFormat = "2006-01-02T15:04:05.000000000Z"

func formatDatabaseTime(value time.Time) string {
	return value.UTC().Format(databaseTimeFormat)
}

func parseDatabaseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
