// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// DefaultKeyedSnapshotEntries is the MaxEntries a zero KeyedSnapshotOptions uses.
const DefaultKeyedSnapshotEntries = 10_000

// KeyedSnapshotOptions bounds a keyed snapshot stored in a poll cursor document.
type KeyedSnapshotOptions struct {
	// MaxEntries bounds tracked items. Zero means DefaultKeyedSnapshotEntries; at most
	// projectconfig.MaxKeyedSnapshotEntries.
	MaxEntries int
	// KeepValues stores each item's SnapshotValue so diffs carry previous values. It costs document
	// space against the 8 MiB bound. Default false.
	KeepValues bool
	// EvictLeastRecentlySeen drops the entries with the lowest Seen counter at MaxEntries instead of
	// failing. It is only for delta feeds (ApplyKeyedSnapshotChanges): a full-listing diff would report
	// evicted keys as added, so DiffKeyedSnapshot rejects it.
	EvictLeastRecentlySeen bool
}

// KeyedItem is a provider item or change a keyed snapshot can track.
type KeyedItem interface {
	// SnapshotKey is the item's stable key, 1 byte to 1 KiB.
	SnapshotKey() string
	// SnapshotHash is the content hash: lowercase hex, 16 to 64 characters, equal for equal content. The
	// recommended hash is the first 8 or 16 bytes of SHA-256 over a canonical encoding of the item.
	SnapshotHash() string
	// SnapshotValue is the item's JSON value, used only with KeepValues; nil omits it.
	SnapshotValue() json.RawMessage
}

// KeyedSnapshotChange is one key whose hash changed between two snapshots.
type KeyedSnapshotChange struct {
	// Previous is the stored entry before the change.
	Previous projectconfig.KeyedSnapshotEntry
	// Current is the entry built from the current item.
	Current projectconfig.KeyedSnapshotEntry
}

// KeyedSnapshotDiff is the difference between a stored snapshot and a full listing of current items.
// Every list is sorted by key.
type KeyedSnapshotDiff struct {
	// Added are current items whose key the stored snapshot lacks.
	Added []projectconfig.KeyedSnapshotEntry
	// Changed are keys whose hash differs.
	Changed []KeyedSnapshotChange
	// Removed are stored entries whose key the listing lacks.
	Removed []projectconfig.KeyedSnapshotEntry
	// Next is the snapshot to return in ChangePage.Next: the current items, sorted.
	Next *projectconfig.KeyedSnapshot
}

func resolveKeyedSnapshotOptions(options KeyedSnapshotOptions) (KeyedSnapshotOptions, error) {
	if options.MaxEntries == 0 {
		options.MaxEntries = DefaultKeyedSnapshotEntries
	}
	if options.MaxEntries < 1 || options.MaxEntries > projectconfig.MaxKeyedSnapshotEntries {
		return options, fmt.Errorf("keyed snapshot MaxEntries must be 1 to %d", projectconfig.MaxKeyedSnapshotEntries)
	}
	return options, nil
}

// DiffKeyedSnapshot compares a full listing of current items with the stored snapshot, for example every
// row of a sheet range for a "row updated" Trigger. A nil previous snapshot is empty. Unchanged entries
// keep their Seen counter; added and changed entries have Seen zero, which the commit replaces with its
// CommitCount. It returns an error wrapping projectconfig.ErrPollCursorTooLarge when items exceed
// MaxEntries, and another error for duplicate keys, a malformed key or hash, or EvictLeastRecentlySeen.
func DiffKeyedSnapshot(previous *projectconfig.KeyedSnapshot, items []KeyedItem, options KeyedSnapshotOptions) (KeyedSnapshotDiff, error) {
	options, err := resolveKeyedSnapshotOptions(options)
	if err != nil {
		return KeyedSnapshotDiff{}, err
	}
	if options.EvictLeastRecentlySeen {
		return KeyedSnapshotDiff{}, errors.New("DiffKeyedSnapshot does not support EvictLeastRecentlySeen")
	}
	if len(items) > options.MaxEntries {
		return KeyedSnapshotDiff{}, fmt.Errorf("%w: %d items exceed %d snapshot entries", projectconfig.ErrPollCursorTooLarge, len(items), options.MaxEntries)
	}
	current, err := keyedSnapshotEntries(items, options)
	if err != nil {
		return KeyedSnapshotDiff{}, err
	}
	stored := storedEntries(previous)
	diff := KeyedSnapshotDiff{Next: &projectconfig.KeyedSnapshot{Entries: current}}
	storedIndex := 0
	for index := range current {
		for storedIndex < len(stored) && stored[storedIndex].Key < current[index].Key {
			diff.Removed = append(diff.Removed, stored[storedIndex])
			storedIndex++
		}
		if storedIndex < len(stored) && stored[storedIndex].Key == current[index].Key {
			if stored[storedIndex].Hash == current[index].Hash {
				current[index].Seen = stored[storedIndex].Seen
			} else {
				diff.Changed = append(diff.Changed, KeyedSnapshotChange{Previous: stored[storedIndex], Current: current[index]})
			}
			storedIndex++
			continue
		}
		diff.Added = append(diff.Added, current[index])
	}
	diff.Removed = append(diff.Removed, stored[storedIndex:]...)
	return diff, nil
}

// ApplyKeyedSnapshotChanges updates a stored snapshot with the partial changes of a delta feed, such as
// Google Drive changes.list: it adds or replaces each upserted item (Seen zero, meaning this commit) and
// deletes removedKeys. A nil previous snapshot is empty and previous is not modified. Over MaxEntries it
// evicts the lowest Seen entries when EvictLeastRecentlySeen is set, and otherwise returns an error
// wrapping projectconfig.ErrPollCursorTooLarge.
func ApplyKeyedSnapshotChanges(previous *projectconfig.KeyedSnapshot, upserted []KeyedItem, removedKeys []string, options KeyedSnapshotOptions) (*projectconfig.KeyedSnapshot, error) {
	options, err := resolveKeyedSnapshotOptions(options)
	if err != nil {
		return nil, err
	}
	changes, err := keyedSnapshotEntries(upserted, options)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]projectconfig.KeyedSnapshotEntry, len(storedEntries(previous))+len(changes))
	for _, entry := range storedEntries(previous) {
		byKey[entry.Key] = entry
	}
	for _, key := range removedKeys {
		delete(byKey, key)
	}
	for _, entry := range changes {
		byKey[entry.Key] = entry
	}
	entries := make([]projectconfig.KeyedSnapshotEntry, 0, len(byKey))
	for _, entry := range byKey {
		entries = append(entries, entry)
	}
	if len(entries) > options.MaxEntries {
		if !options.EvictLeastRecentlySeen {
			return nil, fmt.Errorf("%w: %d entries exceed %d", projectconfig.ErrPollCursorTooLarge, len(entries), options.MaxEntries)
		}
		// Seen zero is this commit, so it is the most recent.
		recency := func(entry projectconfig.KeyedSnapshotEntry) int64 {
			if entry.Seen == 0 {
				return 1<<63 - 1
			}
			return entry.Seen
		}
		sort.Slice(entries, func(i, j int) bool {
			if recency(entries[i]) != recency(entries[j]) {
				return recency(entries[i]) > recency(entries[j])
			}
			return entries[i].Key < entries[j].Key
		})
		entries = entries[:options.MaxEntries]
	}
	slices.SortFunc(entries, func(left, right projectconfig.KeyedSnapshotEntry) int { return strings.Compare(left.Key, right.Key) })
	return &projectconfig.KeyedSnapshot{Entries: entries}, nil
}

// keyedSnapshotEntries converts items to sorted entries with Seen zero and rejects duplicate keys.
func keyedSnapshotEntries(items []KeyedItem, options KeyedSnapshotOptions) ([]projectconfig.KeyedSnapshotEntry, error) {
	entries := make([]projectconfig.KeyedSnapshotEntry, 0, len(items))
	for _, item := range items {
		if item == nil {
			return nil, errors.New("keyed snapshot item is nil")
		}
		entry := projectconfig.KeyedSnapshotEntry{Key: item.SnapshotKey(), Hash: item.SnapshotHash()}
		if options.KeepValues {
			entry.Value = item.SnapshotValue()
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(left, right projectconfig.KeyedSnapshotEntry) int { return strings.Compare(left.Key, right.Key) })
	if err := projectconfig.ValidateKeyedSnapshot(projectconfig.KeyedSnapshot{Entries: entries}); err != nil {
		return nil, fmt.Errorf("keyed snapshot items: %w", err)
	}
	return entries, nil
}

func storedEntries(snapshot *projectconfig.KeyedSnapshot) []projectconfig.KeyedSnapshotEntry {
	if snapshot == nil {
		return nil
	}
	return snapshot.Entries
}

// lookupKeyedSnapshotEntry finds key in a sorted snapshot.
func lookupKeyedSnapshotEntry(snapshot *projectconfig.KeyedSnapshot, key string) *projectconfig.KeyedSnapshotEntry {
	entries := storedEntries(snapshot)
	index, found := slices.BinarySearchFunc(entries, key, func(entry projectconfig.KeyedSnapshotEntry, target string) int {
		return strings.Compare(entry.Key, target)
	})
	if !found {
		return nil
	}
	entry := entries[index]
	return &entry
}
