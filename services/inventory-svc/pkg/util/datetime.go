package util

import "time"

const (
	DateTimeFormat = "2006-01-02 15:04:05"
	ISO8601Format  = "2006-01-02T15:04:05Z"
)

func FormatDateTime(t time.Time) string {
	return t.Format(DateTimeFormat)
}

func ParseDateTime(s string) (time.Time, error) {
	return time.Parse(DateTimeFormat, s)
}

func TimeToISO8601Str(t time.Time) string {
	return t.Format(ISO8601Format)
}

func ParseISO8601(s string) (time.Time, error) {
	return time.Parse(ISO8601Format, s)
}

func GetDefaultTimeZone() *time.Location {
	localTZ, _ := time.LoadLocation("Local")
	return localTZ
}

func Now() time.Time {
	return time.Now().In(GetDefaultTimeZone())
}
