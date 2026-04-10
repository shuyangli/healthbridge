package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/shuyangli/healthbridge/cli/internal/cache"
	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

func newSyncCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sync",
		Short: "Pull anchored deltas of HealthKit samples into the local cache",
		Long: `Builds a sync job that asks the iPhone for everything that has
changed since the last sync, per sample type, using HKAnchoredObjectQuery.
The iPhone streams back result pages keyed to the same job_id; the CLI
applies adds/deletes to its local cache transactionally per page.

Use --type to limit which sample types are synced; default is all
supported types. Use --full to wipe the anchors first and re-pull from
scratch (rarely needed unless the cache has drifted).

Examples:
  healthbridge sync
  healthbridge sync --type step_count
  healthbridge sync --full --type body_mass
`,
		RunE: runSync,
	}
	c.Flags().StringSlice("type", nil, "Limit sync to these sample types (default: all supported)")
	c.Flags().Bool("full", false, "Wipe local anchors before syncing (forces re-pull)")
	return c
}

func runSync(c *cobra.Command, _ []string) error {
	flags, err := commonFromCmd(c)
	if err != nil {
		return err
	}
	typeFilter, _ := c.Flags().GetStringSlice("type")
	full, _ := c.Flags().GetBool("full")

	types := selectSyncTypes(typeFilter)
	if len(types) == 0 {
		return fmt.Errorf("no sample types selected")
	}

	session, authToken, err := loadSession(flags)
	if err != nil {
		return err
	}
	rc := newRelayClient(flags).WithAuthToken(authToken)
	cch, err := openCache()
	if err != nil {
		return err
	}
	defer cch.Close()
	store, err := openJobStore()
	if err != nil {
		return err
	}
	defer store.Close()

	if full {
		for _, t := range types {
			if err := cch.WipeType(flags.PairID, string(t)); err != nil {
				return fmt.Errorf("wipe %s: %w", t, err)
			}
		}
	}

	anchors, err := cch.AllAnchors(flags.PairID)
	if err != nil {
		return err
	}

	// Build the sync job. Anchors are sent as base64 strings on the wire
	// even though they're stored as raw bytes in the cache.
	encodedAnchors := make(map[string]string, len(anchors))
	for k, v := range anchors {
		if !typeIncluded(types, k) {
			continue
		}
		encodedAnchors[k] = base64.StdEncoding.EncodeToString(v)
	}
	job := &health.Job{
		ID:        jobs.NewID(),
		Kind:      health.KindSync,
		CreatedAt: time.Now().UTC(),
		Payload: health.SyncPayload{
			Types:   types,
			Anchors: encodedAnchors,
		},
	}

	ctx, cancel := withCancellableContext()
	defer cancel()

	return executeSyncJob(ctx, c.OutOrStdout(), rc, session, store, cch, job, flags, types)
}

func executeSyncJob(
	ctx context.Context,
	out io.Writer,
	rc *relay.Client,
	session *jobs.Session,
	store *jobs.Store,
	cch *cache.Cache,
	job *health.Job,
	flags commonFlags,
	types []health.SampleType,
) error {
	blob, err := session.SealJob(job)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if err := mirrorEnqueue(store, job, session.PairID); err != nil {
		return err
	}
	if _, err := rc.EnqueueJob(ctx, job.ID, blob, "silent"); err != nil {
		return err
	}

	addedTotal, deletedTotal := 0, 0
	seen := make(map[int]bool)
	deadline := time.Now().Add(resolveSyncWindow(flags))
	for {
		if time.Now().After(deadline) {
			break
		}
		left := time.Until(deadline)
		waitMs := int(left / time.Millisecond)
		if waitMs > relay.DefaultLongPollMs {
			waitMs = relay.DefaultLongPollMs
		}
		if waitMs < 0 {
			waitMs = 0
		}
		resp, err := rc.PollResults(ctx, job.ID, waitMs)
		if err != nil {
			return fmt.Errorf("poll results: %w", err)
		}
		newPages := 0
		for _, r := range resp.Results {
			if seen[r.PageIndex] {
				continue
			}
			seen[r.PageIndex] = true
			newPages++

			result, err := session.OpenResult(r.JobID, r.PageIndex, r.Blob)
			if err != nil {
				return fmt.Errorf("open page %d: %w", r.PageIndex, err)
			}
			if result.Status == health.StatusFailed {
				if result.Error != nil {
					return fmt.Errorf("sync failed at page %d: %s — %s",
						r.PageIndex, result.Error.Code, result.Error.Message)
				}
				return fmt.Errorf("sync failed at page %d", r.PageIndex)
			}
			page, err := decodeSyncPage(result.Result)
			if err != nil {
				return err
			}
			adds, dels, err := applyPage(cch, session.PairID, page)
			if err != nil {
				return err
			}
			addedTotal += adds
			deletedTotal += dels
			if !page.More {
				// Whole multi-page sync is done — ack so the relay
				// prunes any straggler ephemeral pages now instead of
				// waiting on its TTL/alarm sweep.
				ackResult(ctx, rc, job.ID)
				return finishSync(out, store, job, addedTotal, deletedTotal, types, flags.JSON)
			}
		}
		if newPages == 0 {
			// No new pages within the long-poll window — give up on this
			// run; the user can re-invoke later. We do NOT mark the job
			// done in the mirror; it stays pending so `jobs wait` works.
			return emitSyncPending(out, job, addedTotal, deletedTotal, flags.JSON)
		}
	}
	return emitSyncPending(out, job, addedTotal, deletedTotal, flags.JSON)
}

// resolveSyncWindow returns how long the CLI should keep long-polling for
// new sync result pages. Sync can take much longer than a single read,
// so we default to 60s here regardless of --wait.
func resolveSyncWindow(f commonFlags) time.Duration {
	if f.Wait > 0 {
		return f.Wait
	}
	return 60 * time.Second
}

func decodeSyncPage(generic any) (*health.SyncResultPage, error) {
	pb, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("re-marshal page: %w", err)
	}
	var page health.SyncResultPage
	if err := json.Unmarshal(pb, &page); err != nil {
		return nil, fmt.Errorf("decode sync page: %w", err)
	}
	return &page, nil
}

func applyPage(cch *cache.Cache, pairID string, page *health.SyncResultPage) (int, int, error) {
	if err := cch.ApplyAdds(pairID, page.Added); err != nil {
		return 0, 0, fmt.Errorf("apply adds: %w", err)
	}
	if err := cch.ApplyDeletes(pairID, page.Deleted); err != nil {
		return 0, 0, fmt.Errorf("apply deletes: %w", err)
	}
	if page.NextAnchor != "" {
		raw, err := base64.StdEncoding.DecodeString(page.NextAnchor)
		if err != nil {
			return 0, 0, fmt.Errorf("decode next_anchor: %w", err)
		}
		if err := cch.SetAnchor(pairID, string(page.Type), raw); err != nil {
			return 0, 0, err
		}
	}
	return len(page.Added), len(page.Deleted), nil
}

func finishSync(
	out io.Writer,
	store *jobs.Store,
	job *health.Job,
	added, deleted int,
	types []health.SampleType,
	asJSON bool,
) error {
	if store != nil {
		_ = store.MarkDone(job.ID, []byte(fmt.Sprintf(`{"added":%d,"deleted":%d}`, added, deleted)))
	}
	if asJSON {
		return writeJSON(out, map[string]any{
			"job_id":  job.ID,
			"status":  "done",
			"added":   added,
			"deleted": deleted,
			"types":   types,
		})
	}
	_, err := fmt.Fprintf(out, "sync complete: %d added, %d deleted\n", added, deleted)
	return err
}

func emitSyncPending(out io.Writer, job *health.Job, added, deleted int, asJSON bool) error {
	if asJSON {
		return writeJSON(out, map[string]any{
			"job_id":         job.ID,
			"status":         "pending",
			"added_so_far":   added,
			"deleted_so_far": deleted,
		})
	}
	_, err := fmt.Fprintf(out, "sync pending: %d added so far, %d deleted; iPhone hasn't finished draining\n", added, deleted)
	return err
}

// selectSyncTypes returns the supported types narrowed by --type, or all
// of them if no filter is provided.
func selectSyncTypes(filter []string) []health.SampleType {
	if len(filter) == 0 {
		return health.AllSampleTypes()
	}
	var out []health.SampleType
	for _, f := range filter {
		t := health.SampleType(f)
		if t.IsValid() {
			out = append(out, t)
		}
	}
	return out
}

func typeIncluded(types []health.SampleType, candidate string) bool {
	for _, t := range types {
		if string(t) == candidate {
			return true
		}
	}
	return false
}

// openCache opens the SQLite cache at the conventional location.
func openCache() (*cache.Cache, error) {
	if v := os.Getenv("HEALTHBRIDGE_CACHE_DB"); v != "" {
		return cache.Open(v)
	}
	dir := configDir()
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		dir = filepath.Join(v, "healthbridge")
	}
	return cache.Open(filepath.Join(dir, "cache.db"))
}
