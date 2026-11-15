# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Connector Trigger sources that poll a provider change feed with a durable cursor.

A restarted or replaced replica neither misses nor replays a change. A connector supplies a
`ChangeFetcher`: `seed_cursor` returns the provider's current position, and `fetch_changes` returns one
`ChangePage` of changes after a `Cursor`. The kit owns everything else, at behavior parity with the Go
Connector SDK's `sdkgo/pollingtrigger`, and shares its cursor documents byte for byte:

- one cursor document per Trigger binding (`new_binding_poller`) or per shared provider feed
  (`new_feed_poller`), stored with `projectconfig.PollCursorStore` beside the binding's Trigger inbox;
- the ordering rule: the cursor commits past a change only after every member target recorded it (a
  TriggerDeliveryPreparer such as `new_durable_trigger_target`) or consumed it (any other target);
- seeding without backlog on first start, and a logged reseed after `mark_cursor_invalid`;
- one `PollRunner` per provider connection: interval, jitter, per-poller exponential backoff, a
  connection-wide pause for `mark_rate_limited`, an optional `RequestLimiter`, and `wake_poller`;
- an optional bounded keyed snapshot (`diff_keyed_snapshot`, `apply_keyed_snapshot_changes`) committed
  atomically with the cursor, for "row updated" Triggers and delta feeds.

Delivery is at least once. Every Trigger event ID a fetcher emits must be a pure function of the
provider change, so a refetch after a crash between record and commit yields the same IDs; the Dex Flow
target then starts exactly one Flow per event. `examples/polling_trigger.py` wires a complete
application.
"""

from .credential import new_trigger_credential_call
from .cursor_store import CursorStore, new_memory_cursor_store
from .fetcher import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangeFetcher,
    ChangePage,
    Cursor,
    CursorInvalidError,
    RateLimitedError,
    RequestLimiter,
    is_cursor_invalid,
    mark_cursor_invalid,
    mark_rate_limited,
    wait_for_poll_request,
)
from .member import FeedEventSelector, FeedMember, new_feed_member
from .poller import BindingPollerConfig, FeedPollerConfig, Poller, new_binding_poller, new_feed_poller
from .runner import PollRunner, PollRunnerConfig, new_binding_trigger_source
from .schedule import PollSchedule
from .snapshot import (
    DEFAULT_KEYED_SNAPSHOT_ENTRIES,
    KeyedItem,
    KeyedSnapshotChange,
    KeyedSnapshotDiff,
    KeyedSnapshotOptions,
    apply_keyed_snapshot_changes,
    diff_keyed_snapshot,
)

__all__ = [
    "DEFAULT_KEYED_SNAPSHOT_ENTRIES",
    "MAXIMUM_CHANGES_PER_PAGE",
    "BindingPollerConfig",
    "ChangeFetcher",
    "ChangePage",
    "Cursor",
    "CursorInvalidError",
    "CursorStore",
    "FeedEventSelector",
    "FeedMember",
    "FeedPollerConfig",
    "KeyedItem",
    "KeyedSnapshotChange",
    "KeyedSnapshotDiff",
    "KeyedSnapshotOptions",
    "PollRunner",
    "PollRunnerConfig",
    "PollSchedule",
    "Poller",
    "RateLimitedError",
    "RequestLimiter",
    "apply_keyed_snapshot_changes",
    "diff_keyed_snapshot",
    "is_cursor_invalid",
    "mark_cursor_invalid",
    "mark_rate_limited",
    "new_binding_poller",
    "new_binding_trigger_source",
    "new_feed_member",
    "new_feed_poller",
    "new_memory_cursor_store",
    "new_trigger_credential_call",
    "wait_for_poll_request",
]
