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

// canned characteristic answers the fake drainer reflects back. The
// values mirror what the iOS HealthKitMapping helpers will eventually
// return on a real device.
var profileFixtures = map[health.CharacteristicType]string{
	health.CharDateOfBirth:         "1989-03-15",
	health.CharBiologicalSex:       "female",
	health.CharBloodType:           "a_positive",
	health.CharFitzpatrickSkinType: "type_iv",
	health.CharWheelchairUse:       "no",
	health.CharActivityMoveMode:    "active_energy",
}

// TestProfileScenarioCoversEveryCharacteristic exercises the
// `healthbridge profile` subcommand for every supported characteristic
// type. One fakerelay drainer answers all six profile job kinds, and
// the loop asserts the CLI's JSON output contains the expected field
// + value.
func TestProfileScenarioCoversEveryCharacteristic(t *testing.T) {
	server := fakerelay.New()
	defer server.Close()

	pairID := "01J9ZX0PROFILE000000000001"
	session := newScenarioSession(pairID)
	token := server.PreparePair()
	cliClient := relay.New(server.URL(), pairID).WithAuthToken(token)
	drainerClient := relay.New(server.URL(), pairID).WithAuthToken(token)

	handler := func(_ context.Context, job *health.Job) (*health.Result, error) {
		if job.Kind != health.KindProfile {
			return nil, errors.New("expected profile job")
		}
		payloadBytes, err := json.Marshal(job.Payload)
		if err != nil {
			return nil, err
		}
		var pp health.ProfilePayload
		if err := json.Unmarshal(payloadBytes, &pp); err != nil {
			return nil, err
		}
		val, ok := profileFixtures[pp.Field]
		if !ok {
			return &health.Result{
				JobID:  job.ID,
				Status: health.StatusFailed,
				Error:  &health.JobError{Code: "unknown_field", Message: string(pp.Field)},
			}, nil
		}
		return &health.Result{
			JobID:  job.ID,
			Status: health.StatusDone,
			Result: health.ProfileResult{Field: pp.Field, Value: val},
		}, nil
	}

	drainer := fakerelay.NewDrainer(drainerClient, session, handler)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	drainErr := make(chan error, 1)
	go func() { drainErr <- drainer.Run(ctx) }()

	for _, field := range health.AllCharacteristicTypes() {
		t.Run(string(field), func(t *testing.T) {
			job := &health.Job{
				ID:        jobs.NewID(),
				Kind:      health.KindProfile,
				CreatedAt: time.Now().UTC(),
				Payload:   health.ProfilePayload{Field: field},
			}

			var out bytes.Buffer
			cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
			defer ccancel()
			if err := executeProfileJob(cctx, &out, cliClient, session, nil, job, 2*time.Second, true); err != nil {
				t.Fatalf("executeProfileJob(%s): %v", field, err)
			}

			var got struct {
				Status string `json:"status"`
				Field  string `json:"field"`
				Value  string `json:"value"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("decode JSON: %v\n%s", err, out.String())
			}
			if got.Status != "done" {
				t.Errorf("status = %q, want done", got.Status)
			}
			if got.Field != string(field) {
				t.Errorf("field = %q, want %q", got.Field, field)
			}
			if got.Value != profileFixtures[field] {
				t.Errorf("value = %q, want %q", got.Value, profileFixtures[field])
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

// TestProfileRejectsUnknownField asserts the CLI surfaces a clean
// error for an unknown characteristic before sealing or hitting the
// relay.
func TestProfileRejectsUnknownField(t *testing.T) {
	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"profile", "shoe_size"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for unknown characteristic")
	}
	if !strings.Contains(err.Error(), "unknown characteristic") {
		t.Errorf("error = %v, want it to mention 'unknown characteristic'", err)
	}
}
