// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pollingtrigger builds connector Trigger sources that poll a provider change feed with a
// durable cursor, so a restarted or replaced replica neither misses nor replays a change.
//
// A connector supplies a ChangeFetcher: SeedCursor returns the provider's current position, and
// FetchChanges returns one ChangePage of changes after a Cursor. The kit owns everything else:
//
//   - one cursor document per Trigger binding (NewBindingPoller) or per shared provider feed
//     (NewFeedPoller), stored with projectconfig.PollCursorStore beside the binding's Trigger inbox;
//   - the ordering rule: the cursor commits past a change only after every member target recorded
//     it (a target that implements sdkgo.TriggerDeliveryPreparer, such as
//     provider.NewDurableTriggerTarget) or consumed it (any other target);
//   - seeding without backlog on first start, and a logged reseed after MarkCursorInvalid;
//   - one Runner per provider connection: interval, jitter, per-poller exponential backoff, a
//     connection-wide pause for MarkRateLimited, an optional RequestLimiter, and WakePoller;
//   - an optional bounded keyed snapshot (DiffKeyedSnapshot, ApplyKeyedSnapshotChanges) committed
//     atomically with the cursor, for "row updated" Triggers and delta feeds.
//
// Delivery is at least once. Every Trigger event ID a fetcher emits must be a pure function of the
// provider change, so a refetch after a crash between record and commit yields the same IDs; the Dex
// Flow target then starts exactly one Flow per event.
//
// A complete application wires one binding like this, from examples/polling-trigger:
//
//	inbox, err := project.TriggerInbox(key)
//	if err != nil {
//		return err
//	}
//	target, err := provider.NewDurableTriggerTarget(inbox, key, flowTarget, provider.WithTriggerLogger(logger))
//	if err != nil {
//		return err
//	}
//	cursor, err := project.PollCursorStore(projectconfig.PollCursorKey(key))
//	if err != nil {
//		return err
//	}
//	poller, err := pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[ItemChanged]{
//		Definition: definition, Binding: binding, Cursor: cursor,
//		Fetcher: &changeLogFetcher{api: api}, Target: target,
//	})
//	if err != nil {
//		return err
//	}
//	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
//		Connection: connection,
//		Schedule:   pollingtrigger.PollSchedule{Interval: 2 * time.Second, MaxBackoff: time.Minute},
//		Pollers:    []pollingtrigger.Poller{poller},
//		Logger:     logger,
//	})
//	if err != nil {
//		return err
//	}
//	// Replays the inbox, seeds on first start, then polls until ctx ends.
//	return runner.Run(ctx)
//
// A fetcher maps provider statuses to the kit's signals and waits on the runner's limiter before
// every provider request:
//
//	func (fetcher *changeLogFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ItemChanged]], error) {
//		var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ItemChanged]]
//		after, err := strconv.ParseInt(cursor.Token, 10, 64)
//		if err != nil {
//			return page, pollingtrigger.MarkCursorInvalid(fmt.Errorf("change-log cursor is not a sequence"))
//		}
//		if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
//			return page, err
//		}
//		listed, err := fetcher.api.listChanges(ctx, after, 100)
//		if err != nil {
//			return page, fetcher.classify(err) // 410 -> MarkCursorInvalid, 429 -> MarkRateLimited
//		}
//		// ... one sdkgo.TriggerEvent per change, ID "<itemId>@<sequence>" ...
//		return page, nil
//	}
//
// A generated per-Trigger factory that must return an sdkgo.TriggerSource uses
// NewBindingTriggerSource and keeps sdkgo.NewTrigger.
package pollingtrigger
