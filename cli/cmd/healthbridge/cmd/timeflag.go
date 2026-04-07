package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseTimeFlag accepts:
//   - "now"                       → now
//   - RFC3339 timestamps           → as-is
//   - YYYY-MM-DD                   → midnight UTC of that date
//   - relative offsets like "-1d", "-6h", "-30m", "-90s" → now+offset
//
// The relative form is the common one for an agent that wants "the last
// week" without computing dates itself.
func parseTimeFlag(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "now" {
		return now, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if d, err := parseRelative(s); err == nil {
		return now.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("not a valid timestamp or relative offset: %q", s)
}

// parseRelative handles strings like "-1d", "-6h", "-30m", "-90s", "+2h".
// time.ParseDuration doesn't support "d", so we expand it manually.
func parseRelative(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("too short")
	}
	sign := 1
	switch s[0] {
	case '-':
		sign = -1
		s = s[1:]
	case '+':
		s = s[1:]
	}
	// Days suffix.
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(sign) * time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return time.Duration(sign) * d, nil
}
