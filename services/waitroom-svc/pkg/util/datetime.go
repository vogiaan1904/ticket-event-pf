package util

import "time"

const (
	DateTimeFormat = "2006-01-02 15:04:05"
	// Must keep the Z07:00 offset directive: a bare "Z" is a literal to
	// time.Format, so it stamps the local wall clock and labels it UTC.
	ISO8601Format = time.RFC3339
)

func FormatDateTime(t time.Time) string {
	return t.Format(DateTimeFormat)
}

func ParseDateTime(s string) (time.Time, error) {
	return time.Parse(DateTimeFormat, s)
}

func TimeToISO8601Str(t time.Time) string {
	return t.UTC().Format(ISO8601Format)
}

func ParseISO8601(s string) (time.Time, error) {
	return time.Parse(ISO8601Format, s)
}
