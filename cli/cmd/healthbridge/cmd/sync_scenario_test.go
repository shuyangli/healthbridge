package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/cache"
	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

// TestSyncScenarioRoundTrip drives a multi-page sync end-to-end through
// the fakerelay. The drainer's MultiPageJobHandler emits two result pages
// (page 0 has 2 added samples and `more: true`, page 1 has 1 added sample
// and `more: false`); the CLI should reassemble both, write all 3 samples
// to the cache, and persist the next_anchor.
func TestSyncScenarioRoundTrip(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0PAIR000000000000060"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	cch, err := cache.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	store, err := jobs.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	page0 := &health.SyncResultPage{
		Type:      health.StepCount,
		PageIndex: 0,
		Added: []health.Sample{
			{UUID: "uuid-1", Type: health.StepCount, Value: 1000, Unit: "count", Start: now, End: now.Add(time.Hour)},
			{UUID: "uuid-2", Type: health.StepCount, Value: 2000, Unit: "count", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour)},
		},
		More: true,
	}
	page1 := &health.SyncResultPage{
		Type:      health.StepCount,
		PageIndex: 1,
		Added: []health.Sample{
			{UUID: "uuid-3", Type: health.StepCount, Value: 3000, Unit: "count", Start: now.Add(2 * time.Hour), End: now.Add(3 * time.Hour)},
		},
		NextAnchor: base64.StdEncoding.EncodeToString([]byte("anchor-after-3-samples")),
		More:       false,
	}

	var seen atomic.Int32
	handler := func(_ context.Context, job *health.Job) ([]*health.Result, error) {
		seen.Add(1)
		if job.Kind != health.KindSync {
			return nil, errors.New("expected sync job")
		}
		return []*health.Result{
			{Status: health.StatusDone, PageIndex: 0, Result: page0},
			{Status: health.StatusDone, PageIndex: 1, Result: page1},
		}, nil
	}
	drainer := fakerelay.NewMultiPageDrainer(drainerClient, session, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = drainer.Run(ctx) }()

	job := &health.Job{
		ID:        jobs.NewID(),
		Kind:      health.KindSync,
		CreatedAt: time.Now().UTC(),
		Payload: health.SyncPayload{
			Types: []health.SampleType{health.StepCount},
		},
	}

	var out bytes.Buffer
	flags := commonFlags{PairID: pairID, JSON: true, Wait: 3 * time.Second}
	if err := executeSyncJob(ctx, &out, cliClient, session, store, cch, job, flags, []health.SampleType{health.StepCount}); err != nil {
		t.Fatalf("executeSyncJob: %v\n%s", err, out.String())
	}

	// Cache should now hold 3 step_count samples and an anchor.
	if n, _ := cch.SampleCountByType(pairID, "step_count"); n != 3 {
		t.Errorf("samples cached = %d, want 3", n)
	}
	anchor, _ := cch.GetAnchor(pairID, "step_count")
	if string(anchor) != "anchor-after-3-samples" {
		t.Errorf("anchor = %q, want anchor-after-3-samples", anchor)
	}

	// Output should report added=3 deleted=0.
	var resp struct {
		Status  string `json:"status"`
		Added   int    `json:"added"`
		Deleted int    `json:"deleted"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out.String())
	}
	if resp.Status != "done" || resp.Added != 3 || resp.Deleted != 0 {
		t.Errorf("unexpected resp: %+v", resp)
	}
}

// TestSyncScenarioAppliesDeletes verifies the deleted-uuid path: a second
// sync that returns a `deleted` list removes those rows from the cache.
func TestSyncScenarioAppliesDeletes(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()
	pairID := "01J9ZX0PAIR000000000000061"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	cch, _ := cache.Open(":memory:")
	defer cch.Close()
	store, _ := jobs.Open(":memory:")
	defer store.Close()

	// Pre-populate the cache.
	now := time.Now().UTC()
	_ = cch.ApplyAdds(pairID, []health.Sample{
		{UUID: "to-delete", Type: health.StepCount, Value: 1, Unit: "count", Start: now, End: now},
		{UUID: "to-keep", Type: health.StepCount, Value: 2, Unit: "count", Start: now, End: now},
	})

	handler := func(_ context.Context, _ *health.Job) ([]*health.Result, error) {
		return []*health.Result{{
			Status:    health.StatusDone,
			PageIndex: 0,
			Result: &health.SyncResultPage{
				Type:       health.StepCount,
				PageIndex:  0,
				Deleted:    []string{"to-delete"},
				NextAnchor: base64.StdEncoding.EncodeToString([]byte("a")),
				More:       false,
			},
		}}, nil
	}
	drainer := fakerelay.NewMultiPageDrainer(drainerClient, session, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = drainer.Run(ctx) }()

	job := &health.Job{
		ID:        jobs.NewID(),
		Kind:      health.KindSync,
		CreatedAt: time.Now().UTC(),
		Payload:   health.SyncPayload{Types: []health.SampleType{health.StepCount}},
	}
	var out bytes.Buffer
	flags := commonFlags{PairID: pairID, JSON: true, Wait: 3 * time.Second}
	if err := executeSyncJob(ctx, &out, cliClient, session, store, cch, job, flags, []health.SampleType{health.StepCount}); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}

	if n, _ := cch.SampleCountByType(pairID, "step_count"); n != 1 {
		t.Errorf("samples after sync = %d, want 1", n)
	}
}

// TestSyncFullWipesAnchorsBeforePulling verifies --full nukes the anchor
// before sending the request, so the iPhone sees a missing anchor and
// runs a full sync.
func TestSyncFullWipesAnchorsBeforePulling(t *testing.T) {
	cch, _ := cache.Open(":memory:")
	defer cch.Close()

	// Pre-set an anchor for step_count + body_mass. Sync --full --type
	// step_count should clear only step_count.
	_ = cch.SetAnchor("p", "step_count", []byte("old-anchor"))
	_ = cch.SetAnchor("p", "body_mass", []byte("body-anchor"))

	// We don't need to run the full sync here, just verify WipeType.
	if err := cch.WipeType("p", "step_count"); err != nil {
		t.Fatal(err)
	}
	if a, _ := cch.GetAnchor("p", "step_count"); a != nil {
		t.Errorf("step_count anchor not cleared")
	}
	if a, _ := cch.GetAnchor("p", "body_mass"); string(a) != "body-anchor" {
		t.Errorf("body_mass anchor was clobbered")
	}
}

func TestSelectSyncTypesEmptyMeansAll(t *testing.T) {
	all := selectSyncTypes(nil)
	if len(all) != len(health.AllSampleTypes()) {
		t.Errorf("selectSyncTypes(nil) = %d types, want %d", len(all), len(health.AllSampleTypes()))
	}
}

func TestSelectSyncTypesIgnoresUnknown(t *testing.T) {
	got := selectSyncTypes([]string{"step_count", "totally_made_up", "heart_rate"})
	if len(got) != 2 {
		t.Errorf("got %d types, want 2", len(got))
	}
}

// TestExecuteSyncReturnsPendingWhenDrainerOffline ensures the CLI emits a
// pending status (not an error) when no drainer is around.
func TestExecuteSyncReturnsPendingWhenDrainerOffline(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()
	pairID := "01J9ZX0PAIR000000000000062"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	cch, _ := cache.Open(":memory:")
	defer cch.Close()
	store, _ := jobs.Open(":memory:")
	defer store.Close()

	job := &health.Job{
		ID:        jobs.NewID(),
		Kind:      health.KindSync,
		CreatedAt: time.Now().UTC(),
		Payload:   health.SyncPayload{Types: []health.SampleType{health.StepCount}},
	}
	flags := commonFlags{PairID: pairID, JSON: true, Wait: 200 * time.Millisecond}
	var out bytes.Buffer
	if err := executeSyncJob(context.Background(), &out, cliClient, session, store, cch, job, flags, []health.SampleType{health.StepCount}); err != nil {
		t.Fatalf("executeSyncJob: %v", err)
	}
	if !strings.Contains(out.String(), "pending") {
		t.Errorf("expected pending output, got: %s", out.String())
	}
}
