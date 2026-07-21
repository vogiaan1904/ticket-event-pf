package util

import (
	"testing"
	"time"
)

// A trailing bare "Z" is a literal to time.Format, not the Z07:00 offset
// directive. Formatting a non-UTC time with it emits that wall clock labelled
// UTC, which silently shifts the instant by the host's offset.
func TestTimeToISO8601StrDenotesTheSameInstantFromAnyZone(t *testing.T) {
	instant := time.Date(2026, 7, 21, 6, 29, 4, 0, time.UTC)

	zones := map[string]*time.Location{
		"UTC":      time.UTC,
		"UTC+7":    time.FixedZone("+07:00", 7*3600),
		"UTC-5":    time.FixedZone("-05:00", -5*3600),
		"UTC+5:30": time.FixedZone("+05:30", 5*3600+30*60),
	}

	for name, loc := range zones {
		t.Run(name, func(t *testing.T) {
			got := TimeToISO8601Str(instant.In(loc))

			parsed, err := time.Parse(time.RFC3339, got)
			if err != nil {
				t.Fatalf("emitted %q, which is not RFC3339: %v", got, err)
			}

			if !parsed.Equal(instant) {
				t.Errorf("emitted %q => %v, want the original instant %v",
					got, parsed.UTC(), instant)
			}
		})
	}
}

// Formatting then parsing must be a fixed point. With the literal-Z layout a
// non-UTC value drifted by the offset on every round trip.
func TestISO8601RoundTripDoesNotDrift(t *testing.T) {
	start := time.Date(2026, 7, 21, 6, 29, 4, 0, time.FixedZone("+07:00", 7*3600))

	current := TimeToISO8601Str(start)
	for i := range 3 {
		parsed, err := ParseISO8601(current)
		if err != nil {
			t.Fatalf("round %d: parse %q: %v", i, current, err)
		}

		if !parsed.Equal(start) {
			t.Fatalf("round %d: %q => %v, drifted from %v",
				i, current, parsed.UTC(), start.UTC())
		}

		current = TimeToISO8601Str(parsed)
	}
}

// Widening the layout must not reject anything it used to accept.
func TestParseISO8601AcceptsLegacyAndOffsetForms(t *testing.T) {
	want := time.Date(2026, 7, 21, 6, 29, 4, 0, time.UTC)

	tests := map[string]string{
		"legacy trailing Z":  "2026-07-21T06:29:04Z",
		"explicit +07:00":    "2026-07-21T13:29:04+07:00",
		"explicit -05:00":    "2026-07-21T01:29:04-05:00",
		"fractional seconds": "2026-07-21T06:29:04.123Z",
		"zero offset as +00": "2026-07-21T06:29:04+00:00",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ParseISO8601(input)
			if err != nil {
				t.Fatalf("parse %q: %v", input, err)
			}

			if name == "fractional seconds" {
				got = got.Truncate(time.Second)
			}

			if !got.Equal(want) {
				t.Errorf("parse %q = %v, want %v", input, got.UTC(), want)
			}
		})
	}
}

func TestParseISO8601RejectsNonTimestamps(t *testing.T) {
	for _, input := range []string{"", "not a time", "2026-07-21 13:29:04.148 +0700 +07"} {
		if _, err := ParseISO8601(input); err == nil {
			t.Errorf("parse %q unexpectedly succeeded", input)
		}
	}
}
