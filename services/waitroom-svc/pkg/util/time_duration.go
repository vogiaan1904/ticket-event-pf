package util

import "time"

func ParseDuration(duration string) (time.Duration, error) {
	return time.ParseDuration(duration)
}
