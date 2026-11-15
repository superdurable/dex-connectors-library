// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * Connector Trigger sources that poll a provider change feed with a durable cursor, so a restarted or
 * replaced replica neither misses nor replays a change. A connector supplies a ChangeFetcher; the kit owns
 * the cursor document, the record-before-commit ordering, seeding without backlog, reseeding after
 * markCursorInvalid, the per-connection Runner schedule, and bounded keyed snapshots. It matches the Go
 * Connector SDK's sdkgo/pollingtrigger package and shares its stored documents byte for byte.
 */
export { newMemoryCursorStore, type CursorStore } from './cursor_store.js';
export { newTriggerCredentialCall, triggerCredentialCallId } from './credential.js';
export {
  MaxChangesPerPage, RateLimitedError, isCursorInvalid, markCursorInvalid, markRateLimited, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type RequestLimiter,
} from './fetcher.js';
export { newFeedMember, type FeedEventSelector, type FeedMember } from './member.js';
export type { Poller } from './poller.js';
export { newBindingPoller, newBindingTriggerSource, newFeedPoller, type BindingPollerConfig, type FeedPollerConfig } from './poller_config.js';
export { newRunner, type PollSchedule, type Runner, type RunnerConfig } from './runner.js';
export {
  DefaultKeyedSnapshotEntries, applyKeyedSnapshotChanges, diffKeyedSnapshot,
  type KeyedItem, type KeyedSnapshotChange, type KeyedSnapshotDiff, type KeyedSnapshotOptions,
} from './snapshot.js';
