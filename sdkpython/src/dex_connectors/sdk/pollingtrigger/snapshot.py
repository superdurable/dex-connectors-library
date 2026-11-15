# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded keyed snapshots stored with a poll cursor, for "row updated" Triggers and delta feeds."""

import bisect
from collections.abc import Sequence
from dataclasses import dataclass, field, replace
from typing import Final, Protocol, runtime_checkable

from ..projectconfig.poll_cursor import (
    MAXIMUM_KEYED_SNAPSHOT_ENTRIES,
    KeyedSnapshot,
    KeyedSnapshotEntry,
    PollCursorTooLargeError,
    _go_order,
    validate_keyed_snapshot,
)

DEFAULT_KEYED_SNAPSHOT_ENTRIES: Final = 10_000
"""The `max_entries` a default KeyedSnapshotOptions uses."""

_MOST_RECENT_SEEN = (1 << 63) - 1


@dataclass(frozen=True, slots=True)
class KeyedSnapshotOptions:
    """Bounds a keyed snapshot stored in a poll cursor document."""

    max_entries: int = 0
    """Bound of tracked items; zero means 10 000, at most 100 000."""
    keep_values: bool = False
    """Store each item's `snapshot_value` so diffs carry previous values; it costs document space."""
    evict_least_recently_seen: bool = False
    """Drop the lowest-`seen` entries at `max_entries` instead of failing.

    Only for delta feeds (`apply_keyed_snapshot_changes`): a full-listing diff would report evicted keys
    as added, so `diff_keyed_snapshot` rejects it.
    """


@runtime_checkable
class KeyedItem(Protocol):
    """A provider item or change a keyed snapshot can track."""

    def snapshot_key(self) -> str:
        """The item's stable key, 1 byte to 1 KiB of UTF-8."""
        ...

    def snapshot_hash(self) -> str:
        """The content hash: lowercase hex, 16 to 64 characters, equal for equal content.

        The recommended hash is the first 8 or 16 bytes of SHA-256 over a canonical encoding of the item.
        """
        ...

    def snapshot_value(self) -> bytes | None:
        """The item's raw JSON value, used only with `keep_values`; None omits it."""
        ...


@dataclass(frozen=True, slots=True)
class KeyedSnapshotChange:
    """One key whose hash changed between two snapshots."""

    previous: KeyedSnapshotEntry
    """The stored entry before the change."""
    current: KeyedSnapshotEntry
    """The entry built from the current item."""


@dataclass(frozen=True, slots=True)
class KeyedSnapshotDiff:
    """The difference between a stored snapshot and a full listing of current items, each list by key."""

    added: tuple[KeyedSnapshotEntry, ...] = ()
    """Current items whose key the stored snapshot lacks."""
    changed: tuple[KeyedSnapshotChange, ...] = ()
    """Keys whose hash differs."""
    removed: tuple[KeyedSnapshotEntry, ...] = ()
    """Stored entries whose key the listing lacks."""
    next_snapshot: KeyedSnapshot = field(default_factory=KeyedSnapshot)
    """The snapshot to return in `ChangePage.next_cursor`: the current items, sorted."""


def _resolve_keyed_snapshot_options(options: KeyedSnapshotOptions) -> KeyedSnapshotOptions:
    if options.max_entries == 0:
        options = replace(options, max_entries=DEFAULT_KEYED_SNAPSHOT_ENTRIES)
    if not 1 <= options.max_entries <= MAXIMUM_KEYED_SNAPSHOT_ENTRIES:
        raise ValueError(f"keyed snapshot max_entries must be 1 to {MAXIMUM_KEYED_SNAPSHOT_ENTRIES}")
    return options


def diff_keyed_snapshot(
    previous: KeyedSnapshot | None, items: Sequence[KeyedItem], options: KeyedSnapshotOptions
) -> KeyedSnapshotDiff:
    """Compare a full listing of current items with the stored snapshot, such as every row of a sheet.

    None is an empty snapshot. Unchanged entries keep their `seen` counter; added and changed entries
    have `seen` zero, which the commit replaces with its commit count. Raises PollCursorTooLargeError
    when items exceed `max_entries`, and ValueError for duplicate keys, a malformed key or hash, or
    `evict_least_recently_seen`.
    """
    options = _resolve_keyed_snapshot_options(options)
    if options.evict_least_recently_seen:
        raise ValueError("diff_keyed_snapshot does not support evict_least_recently_seen")
    if len(items) > options.max_entries:
        raise PollCursorTooLargeError(
            f"project poll cursor state exceeds its bound: {len(items)} items exceed "
            f"{options.max_entries} snapshot entries"
        )
    current = _keyed_snapshot_entries(items, options)
    stored = previous.entries if previous is not None else ()
    added: list[KeyedSnapshotEntry] = []
    changed: list[KeyedSnapshotChange] = []
    removed: list[KeyedSnapshotEntry] = []
    stored_index = 0
    for index, entry in enumerate(current):
        order = _go_order(entry.key)
        while stored_index < len(stored) and _go_order(stored[stored_index].key) < order:
            removed.append(stored[stored_index])
            stored_index += 1
        if stored_index < len(stored) and stored[stored_index].key == entry.key:
            if stored[stored_index].hash == entry.hash:
                current[index] = replace(entry, seen=stored[stored_index].seen)
            else:
                changed.append(KeyedSnapshotChange(stored[stored_index], entry))
            stored_index += 1
            continue
        added.append(entry)
    removed.extend(stored[stored_index:])
    return KeyedSnapshotDiff(tuple(added), tuple(changed), tuple(removed), KeyedSnapshot(tuple(current)))


def apply_keyed_snapshot_changes(
    previous: KeyedSnapshot | None,
    upserted: Sequence[KeyedItem],
    removed_keys: Sequence[str],
    options: KeyedSnapshotOptions,
) -> KeyedSnapshot:
    """Update a stored snapshot with the partial changes of a delta feed, such as Drive changes.list.

    It adds or replaces each upserted item (`seen` zero, meaning this commit) and deletes removed_keys.
    None is an empty snapshot; previous is never modified. Over `max_entries` it evicts the lowest-`seen`
    entries when `evict_least_recently_seen` is set, and otherwise raises PollCursorTooLargeError.
    """
    options = _resolve_keyed_snapshot_options(options)
    changes = _keyed_snapshot_entries(upserted, options)
    by_key = {entry.key: entry for entry in (previous.entries if previous is not None else ())}
    for key in removed_keys:
        by_key.pop(key, None)
    for entry in changes:
        by_key[entry.key] = entry
    entries = list(by_key.values())
    if len(entries) > options.max_entries:
        if not options.evict_least_recently_seen:
            raise PollCursorTooLargeError(
                f"project poll cursor state exceeds its bound: {len(entries)} entries exceed "
                f"{options.max_entries}"
            )
        # Seen zero is this commit, so it is the most recent.
        entries.sort(key=lambda entry: _go_order(entry.key))
        entries.sort(key=lambda entry: entry.seen or _MOST_RECENT_SEEN, reverse=True)
        entries = entries[: options.max_entries]
    entries.sort(key=lambda entry: _go_order(entry.key))
    return KeyedSnapshot(tuple(entries))


def _keyed_snapshot_entries(
    items: Sequence[KeyedItem], options: KeyedSnapshotOptions
) -> list[KeyedSnapshotEntry]:
    entries: list[KeyedSnapshotEntry] = []
    for item in items:
        if item is None:
            raise ValueError("keyed snapshot item is None")
        value = item.snapshot_value() if options.keep_values else None
        entries.append(KeyedSnapshotEntry(item.snapshot_key(), item.snapshot_hash(), 0, value))
    entries.sort(key=lambda entry: _go_order(entry.key))
    try:
        validate_keyed_snapshot(KeyedSnapshot(tuple(entries)))
    except ValueError as error:
        raise type(error)(f"keyed snapshot items: {error}") from error
    return entries


def _lookup_keyed_snapshot_entry(snapshot: KeyedSnapshot | None, key: str) -> KeyedSnapshotEntry | None:
    if snapshot is None:
        return None
    entries = snapshot.entries
    order = _go_order(key)
    index = bisect.bisect_left(entries, order, key=lambda entry: _go_order(entry.key))
    if index < len(entries) and entries[index].key == key:
        return entries[index]
    return None
