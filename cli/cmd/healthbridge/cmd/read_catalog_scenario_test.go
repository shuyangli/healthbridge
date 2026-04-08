package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

// TestReadScenarioCoversEntireCatalog runs the executeReadJob path
// once for every entry in cli/internal/health.Catalog and asserts the
// CLI accepts the wire-format type, the drainer's synthetic sample
// round-trips with the canonical unit, and the JSON output decodes.
//
// One fakerelay + one drainer goroutine handles every job — the
// drainer is type-agnostic and just echoes back a single sample of
// whatever type the job asked for. This is the Layer-5 guard from
// cli/docs/quantity-type-coverage.md: any catalog entry that the
// CLI's argument parser, the codec, or the JSON output path can't
// handle will fail loudly here.
func TestReadScenarioCoversEntireCatalog(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0CATRG00000000000001"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	handler := func(_ context.Context, job *health.Job) (*health.Result, error) {
		if job.Kind != health.KindRead {
			return nil, errors.New("expected read job")
		}
		// Decode the read payload to discover the requested type, then
		// reflect a single canonical sample back. Reuses the typed
		// payload codec the production drainer uses.
		payloadBytes, err := json.Marshal(job.Payload)
		if err != nil {
			return nil, err
		}
		var rp health.ReadPayload
		if err := json.Unmarshal(payloadBytes, &rp); err != nil {
			return nil, err
		}
		d := health.LookupByWire(rp.Type)
		unit := "s" // sleep_analysis / workout fallback
		if d != nil {
			unit = d.Unit
		}
		return &health.Result{
			JobID:     job.ID,
			PageIndex: 0,
			Status:    health.StatusDone,
			Result: health.ReadResult{
				Type: rp.Type,
				Samples: []health.Sample{
					{
						Type:  rp.Type,
						Value: 1.5,
						Unit:  unit,
						Start: rp.From,
						End:   rp.To,
					},
				},
			},
		}, nil
	}

	drainer := fakerelay.NewDrainer(drainerClient, session, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	drainErr := make(chan error, 1)
	go func() { drainErr <- drainer.Run(ctx) }()

	// Iterate every catalog entry. We deliberately exclude the
	// non-quantity carryover (sleep_analysis, workout) here — they
	// have their own scenario coverage and don't share the same
	// "value+unit" model that read returns.
	for _, d := range health.Catalog {
		t.Run(string(d.Wire), func(t *testing.T) {
			job := jobs.NewReadJob(
				d.Wire,
				time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
				time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
			)

			var out bytes.Buffer
			cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
			defer ccancel()
			if err := executeReadJob(cctx, &out, cliClient, session, nil, job, 2*time.Second, true /* json */); err != nil {
				t.Fatalf("executeReadJob(%s): %v", d.Wire, err)
			}

			var got struct {
				Status  string          `json:"status"`
				Type    string          `json:"type"`
				Samples []health.Sample `json:"samples"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("decode JSON for %s: %v\n%s", d.Wire, err, out.String())
			}
			if got.Status != "done" {
				t.Fatalf("status for %s = %q, want done\n%s", d.Wire, got.Status, out.String())
			}
			if got.Type != string(d.Wire) {
				t.Errorf("type round-trip for %s = %q", d.Wire, got.Type)
			}
			if len(got.Samples) != 1 {
				t.Fatalf("samples len for %s = %d, want 1", d.Wire, len(got.Samples))
			}
			if got.Samples[0].Unit != d.Unit {
				t.Errorf("unit round-trip for %s = %q, want %q", d.Wire, got.Samples[0].Unit, d.Unit)
			}
			if got.Samples[0].Type != d.Wire {
				t.Errorf("sample.type round-trip for %s = %q", d.Wire, got.Samples[0].Type)
			}
		})
	}

	cancel()
	if err := <-drainErr; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		if !strings.Contains(err.Error(), "context") {
			t.Fatalf("drainer: %v", err)
		}
	}
}
