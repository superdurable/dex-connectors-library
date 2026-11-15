// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { MaxKeyedSnapshotEntries, PollCursorTooLargeError, compareKeyedSnapshotKeys, validateKeyedSnapshot, type KeyedSnapshot, type KeyedSnapshotEntry } from '../projectconfig/poll_cursor.js';

/** The maxEntries an omitted or zero KeyedSnapshotOptions.maxEntries uses. */
export const DefaultKeyedSnapshotEntries = 10_000;

/** Bounds a keyed snapshot stored in a poll cursor document. */
export interface KeyedSnapshotOptions {
  /** Bound of tracked items. Omitted or zero means DefaultKeyedSnapshotEntries; at most MaxKeyedSnapshotEntries. */ maxEntries?: number;
  /** Store each item's snapshotValue so diffs carry previous values. It costs document space against the 8 MiB bound. Default false. */ keepValues?: boolean;
  /**
   * Drop the entries with the lowest seen counter at maxEntries instead of failing. Only for delta feeds
   * (applyKeyedSnapshotChanges): a full-listing diff would report evicted keys as added, so diffKeyedSnapshot
   * rejects it. Default false.
   */
  evictLeastRecentlySeen?: boolean;
}

/** A provider item or change a keyed snapshot can track. A feed change that has these methods receives its entry in selectors. */
export interface KeyedItem {
  /** The item's stable key, 1 byte to 1 KiB of UTF-8. */ snapshotKey(): string;
  /**
   * The content hash: lowercase hex, 16 to 64 characters, equal for equal content. The recommended hash is
   * the first 8 or 16 bytes of SHA-256 over a canonical encoding of the item.
   */
  snapshotHash(): string;
  /** The item's raw JSON bytes, used only with keepValues; undefined omits it. */ snapshotValue(): Uint8Array | undefined;
}

/** One key whose hash changed between two snapshots. */
export interface KeyedSnapshotChange {
  /** The stored entry before the change. */ previous: KeyedSnapshotEntry;
  /** The entry built from the current item. */ current: KeyedSnapshotEntry;
}

/** The difference between a stored snapshot and a full listing of current items. Every list is sorted by key. */
export interface KeyedSnapshotDiff {
  /** Current items whose key the stored snapshot lacks. */ added: KeyedSnapshotEntry[];
  /** Keys whose hash differs. */ changed: KeyedSnapshotChange[];
  /** Stored entries whose key the listing lacks. */ removed: KeyedSnapshotEntry[];
  /** The snapshot to return in ChangePage.next: the current items, sorted. */ next: KeyedSnapshot;
}

/** KeyedSnapshotOptions with every default applied. */
export interface ResolvedKeyedSnapshotOptions { maxEntries: number; keepValues: boolean; evictLeastRecentlySeen: boolean }

/** Apply defaults and check the bound; throws RangeError for an invalid maxEntries. */
export function resolveKeyedSnapshotOptions(options: KeyedSnapshotOptions): ResolvedKeyedSnapshotOptions {
  const maxEntries = options.maxEntries || DefaultKeyedSnapshotEntries;
  if (!Number.isInteger(maxEntries) || maxEntries < 1 || maxEntries > MaxKeyedSnapshotEntries) throw new RangeError(`keyed snapshot maxEntries must be 1 to ${MaxKeyedSnapshotEntries}`);
  return { maxEntries, keepValues: options.keepValues ?? false, evictLeastRecentlySeen: options.evictLeastRecentlySeen ?? false };
}

/**
 * Compare a full listing of current items with the stored snapshot, for example every row of a sheet range
 * for a "row updated" Trigger. An undefined previous snapshot is empty. Unchanged entries keep their seen
 * counter; added and changed entries have seen zero, which the commit replaces with its commitCount. Throws
 * PollCursorTooLargeError when items exceed maxEntries, and another error for duplicate keys, a malformed
 * key or hash, or evictLeastRecentlySeen.
 */
export function diffKeyedSnapshot(previous: KeyedSnapshot | undefined, items: KeyedItem[], options: KeyedSnapshotOptions = {}): KeyedSnapshotDiff {
  const resolved = resolveKeyedSnapshotOptions(options);
  if (resolved.evictLeastRecentlySeen) throw new TypeError('diffKeyedSnapshot does not support evictLeastRecentlySeen');
  if (items.length > resolved.maxEntries) throw new PollCursorTooLargeError(`${items.length} items exceed ${resolved.maxEntries} snapshot entries`);
  const current = keyedSnapshotEntries(items, resolved);
  const stored = previous?.entries ?? [];
  const diff: KeyedSnapshotDiff = { added: [], changed: [], removed: [], next: { entries: current } };
  let storedIndex = 0;
  for (const [index, entry] of current.entries()) {
    while (storedIndex < stored.length && compareKeyedSnapshotKeys(stored[storedIndex]!.key, entry.key) < 0) diff.removed.push(stored[storedIndex++]!);
    const match = stored[storedIndex];
    if (match !== undefined && match.key === entry.key) {
      if (match.hash === entry.hash) current[index] = { ...entry, seen: match.seen };
      else diff.changed.push({ previous: match, current: entry });
      storedIndex++;
      continue;
    }
    diff.added.push(entry);
  }
  diff.removed.push(...stored.slice(storedIndex));
  return diff;
}

/**
 * Update a stored snapshot with the partial changes of a delta feed, such as Google Drive changes.list: add
 * or replace each upserted item (seen zero, meaning this commit) and delete removedKeys. An undefined previous
 * snapshot is empty and previous is not modified. Over maxEntries it evicts the lowest seen entries when
 * evictLeastRecentlySeen is set, and otherwise throws PollCursorTooLargeError.
 */
export function applyKeyedSnapshotChanges(previous: KeyedSnapshot | undefined, upserted: KeyedItem[], removedKeys: string[], options: KeyedSnapshotOptions = {}): KeyedSnapshot {
  const resolved = resolveKeyedSnapshotOptions(options);
  const changes = keyedSnapshotEntries(upserted, resolved);
  const byKey = new Map<string, KeyedSnapshotEntry>();
  for (const entry of previous?.entries ?? []) byKey.set(entry.key, entry);
  for (const key of removedKeys) byKey.delete(key);
  for (const entry of changes) byKey.set(entry.key, entry);
  let entries = [...byKey.values()];
  if (entries.length > resolved.maxEntries) {
    if (!resolved.evictLeastRecentlySeen) throw new PollCursorTooLargeError(`${entries.length} entries exceed ${resolved.maxEntries}`);
    // Seen zero is this commit, so it is the most recent.
    const recency = (entry: KeyedSnapshotEntry) => entry.seen === 0 ? Number.MAX_SAFE_INTEGER : entry.seen;
    entries.sort((left, right) => recency(right) - recency(left) || compareKeyedSnapshotKeys(left.key, right.key));
    entries = entries.slice(0, resolved.maxEntries);
  }
  entries.sort((left, right) => compareKeyedSnapshotKeys(left.key, right.key));
  return { entries };
}

/** Convert items to sorted entries with seen zero and reject duplicate keys. */
function keyedSnapshotEntries(items: KeyedItem[], options: ResolvedKeyedSnapshotOptions): KeyedSnapshotEntry[] {
  const entries = items.map(item => {
    if (!item) throw new TypeError('keyed snapshot item is missing');
    const entry: KeyedSnapshotEntry = { key: item.snapshotKey(), hash: item.snapshotHash(), seen: 0 };
    if (options.keepValues) {
      const value = item.snapshotValue();
      if (value !== undefined) entry.value = value;
    }
    return entry;
  });
  entries.sort((left, right) => compareKeyedSnapshotKeys(left.key, right.key));
  try { validateKeyedSnapshot({ entries }); }
  catch (error: unknown) {
    if (error instanceof PollCursorTooLargeError) throw error;
    throw new TypeError(`keyed snapshot items: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  return entries;
}

/** Find key in a sorted snapshot, or return undefined. */
export function lookupKeyedSnapshotEntry(snapshot: KeyedSnapshot | undefined, key: string): KeyedSnapshotEntry | undefined {
  const entries = snapshot?.entries ?? [];
  let low = 0, high = entries.length;
  while (low < high) {
    const middle = (low + high) >>> 1;
    const order = compareKeyedSnapshotKeys(entries[middle]!.key, key);
    if (order === 0) return { ...entries[middle]! };
    if (order < 0) low = middle + 1; else high = middle;
  }
  return undefined;
}

/** Report whether a change implements KeyedItem. */
export function isKeyedItem(change: unknown): change is KeyedItem {
  if (typeof change !== 'object' || change === null) return false;
  const candidate = change as Partial<KeyedItem>;
  return typeof candidate.snapshotKey === 'function' && typeof candidate.snapshotHash === 'function' && typeof candidate.snapshotValue === 'function';
}
