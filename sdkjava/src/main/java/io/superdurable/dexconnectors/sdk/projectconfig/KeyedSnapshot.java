// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * A bounded set of tracked provider items stored with a poll cursor, sorted by unique key.
 */
public final class KeyedSnapshot {
    private final List<KeyedSnapshotEntry> entries;

    /**
     * Creates a snapshot.
     *
     * @param entries the items, sorted ascending by key in UTF-8 byte order
     */
    public KeyedSnapshot(final List<KeyedSnapshotEntry> entries) {
        this.entries = Collections.unmodifiableList(new ArrayList<>(entries));
    }

    /**
     * Returns the tracked items.
     *
     * @return the unmodifiable entries
     */
    public List<KeyedSnapshotEntry> getEntries() {
        return entries;
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        return other instanceof KeyedSnapshot && entries.equals(((KeyedSnapshot) other).entries);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return entries.hashCode();
    }
}
