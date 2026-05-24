package logs

import (
	"context"
	"sort"
	"sync"
)

// MergeHistory takes per-job history slices and returns the merged
// stream sorted by (timestamp, JobRID, sequence). Per-job slices come
// in pre-sorted by sequence (that's what LogStore.History guarantees),
// so the resulting order is deterministic and reproducible across two
// readers of the same data.
//
// limit > 0 trims to the most recent `limit` entries — matching the
// "tail" semantics callers expect from a JSON history endpoint.
func MergeHistory(slices [][]LogEntry, limit int) []LogEntry {
	total := 0
	for _, s := range slices {
		total += len(s)
	}
	all := make([]LogEntry, 0, total)
	for _, s := range slices {
		all = append(all, s...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].TS.Equal(all[j].TS) {
			return all[i].TS.Before(all[j].TS)
		}
		if all[i].JobRID != all[j].JobRID {
			return all[i].JobRID < all[j].JobRID
		}
		return all[i].Sequence < all[j].Sequence
	})
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

// FanInLive multiplexes multiple per-job subscription channels into a
// single live stream. The returned channel closes after every input
// channel closes — i.e. after every job's subscription tears down
// (typically because ctx was canceled).
//
// The caller is responsible for invoking the returned cancel function;
// it unsubscribes from every per-job subscriber and drains the channel.
// Safe to call multiple times.
func FanInLive(ctx context.Context, sub LogSubscriber, jobRIDs []string) (<-chan LogEntry, func(), error) {
	out := make(chan LogEntry, 32)
	cancels := make([]func(), 0, len(jobRIDs))
	var wg sync.WaitGroup

	for _, jobRID := range jobRIDs {
		ch, cancel, err := sub.Subscribe(ctx, jobRID)
		if err != nil {
			// Roll back any subscriptions already established before
			// the failing one so we don't leak.
			for _, c := range cancels {
				c()
			}
			close(out)
			return nil, func() {}, err
		}
		cancels = append(cancels, cancel)
		wg.Add(1)
		go func(c <-chan LogEntry) {
			defer wg.Done()
			for entry := range c {
				select {
				case out <- entry:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	var once sync.Once
	teardown := func() {
		once.Do(func() {
			for _, c := range cancels {
				c()
			}
		})
	}
	return out, teardown, nil
}
