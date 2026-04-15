package cache

import (
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

func newCache(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func sample(uuid string, value float64, start time.Time) health.Sample {
	return health.Sample{
		UUID:  uuid,
		Type:  health.StepCount,
		Value: value,
		Unit:  "count",
		Start: start,
		End:   start.Add(time.Hour),
	}
}

func TestApplyAddsThenDeletes(t *testing.T) {
	c := newCache(t)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	adds := []health.Sample{
		sample("uuid-a", 100, now),
		sample("uuid-b", 200, now.Add(time.Hour)),
		sample("uuid-c", 300, now.Add(2*time.Hour)),
	}
	if err := c.ApplyAdds("p", adds); err != nil {
		t.Fatal(err)
	}
	n, err := c.SampleCount("p")
	if err != nil || n != 3 {
		t.Fatalf("count = %d, want 3 (err=%v)", n, err)
	}

	// Re-apply the same UUIDs with new values to test upsert.
	updated := []health.Sample{sample("uuid-a", 999, now)}
	if err := c.ApplyAdds("p", updated); err != nil {
		t.Fatal(err)
	}
	n, _ = c.SampleCount("p")
	if n != 3 {
		t.Errorf("count after upsert = %d, want 3", n)
	}

	if err := c.ApplyDeletes("p", []string{"uuid-b", "uuid-c"}); err != nil {
		t.Fatal(err)
	}
	n, _ = c.SampleCount("p")
	if n != 1 {
		t.Errorf("count after delete = %d, want 1", n)
	}
}

func TestApplyAddsRequiresUUID(t *testing.T) {
	c := newCache(t)
	if err := c.ApplyAdds("p", []health.Sample{{Type: health.StepCount, Unit: "count"}}); err == nil {
		t.Error("expected error for missing UUID")
	}
}

func TestSampleCountByType(t *testing.T) {
	c := newCache(t)
	now := time.Now()
	_ = c.ApplyAdds("p", []health.Sample{
		sample("a", 1, now),
		{UUID: "b", Type: health.HeartRate, Value: 60, Unit: "count/min", Start: now, End: now},
	})
	stepN, _ := c.SampleCountByType("p", "step_count")
	if stepN != 1 {
		t.Errorf("step_count count = %d, want 1", stepN)
	}
	hrN, _ := c.SampleCountByType("p", "heart_rate")
	if hrN != 1 {
		t.Errorf("heart_rate count = %d, want 1", hrN)
	}
}

func TestWipe(t *testing.T) {
	c := newCache(t)
	now := time.Now()
	_ = c.ApplyAdds("p", []health.Sample{sample("a", 1, now)})
	if err := c.Wipe("p"); err != nil {
		t.Fatal(err)
	}
	n, _ := c.SampleCount("p")
	if n != 0 {
		t.Errorf("count after Wipe = %d, want 0", n)
	}
}
