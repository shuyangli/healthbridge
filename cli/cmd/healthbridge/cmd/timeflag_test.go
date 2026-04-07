package cmd

import (
	"testing"
	"time"
)

func TestParseTimeFlag(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"now", now, false},
		{"", now, false},
		{"-1d", now.Add(-24 * time.Hour), false},
		{"-6h", now.Add(-6 * time.Hour), false},
		{"-30m", now.Add(-30 * time.Minute), false},
		{"+2h", now.Add(2 * time.Hour), false},
		{"-7d", now.Add(-7 * 24 * time.Hour), false},
		{"2026-04-01", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), false},
		{"2026-04-01T08:00:00Z", time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC), false},
		{"banana", time.Time{}, true},
		{"-", time.Time{}, true},
		{"-1q", time.Time{}, true},
	}
	for _, tc := range cases {
		got, err := parseTimeFlag(tc.in, now)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTimeFlag(%q): expected error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseTimeFlag(%q): unexpected error %v", tc.in, err)
			continue
		}
		if !got.Equal(tc.want) {
			t.Errorf("parseTimeFlag(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
