// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.Arrays;
import java.util.Objects;

/**
 * One tracked provider item in a keyed snapshot.
 */
public final class KeyedSnapshotEntry {
    private final String key;
    private final String hash;
    private final long seen;
    private final byte[] value;

    /**
     * Creates an entry without a stored value.
     *
     * @param key the connector's stable item key, 1 byte to 1 KiB of UTF-8
     * @param hash the item's lowercase hexadecimal content hash, 16 to 64 characters
     */
    public KeyedSnapshotEntry(final String key, final String hash) {
        this(key, hash, 0, null);
    }

    /**
     * Creates an entry.
     *
     * @param key the connector's stable item key, 1 byte to 1 KiB of UTF-8
     * @param hash the item's lowercase hexadecimal content hash, 16 to 64 characters
     * @param seen the commit count that last added, changed, or reported the entry; 0 means this commit
     * @param value the item's raw JSON value other than null, or {@code null} when not stored
     */
    public KeyedSnapshotEntry(final String key, final String hash, final long seen, final byte[] value) {
        this.key = key == null ? "" : key;
        this.hash = hash == null ? "" : hash;
        this.seen = seen;
        this.value = value == null ? null : value.clone();
    }

    /**
     * Returns the item key.
     *
     * @return the key
     */
    public String getKey() {
        return key;
    }

    /**
     * Returns the content hash.
     *
     * @return the hash
     */
    public String getHash() {
        return hash;
    }

    /**
     * Returns the commit count that last added, changed, or reported the entry.
     *
     * @return the seen counter
     */
    public long getSeen() {
        return seen;
    }

    /**
     * Returns a copy of the raw JSON value.
     *
     * @return the value, or {@code null} when not stored
     */
    public byte[] getValue() {
        return value == null ? null : value.clone();
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof KeyedSnapshotEntry)) {
            return false;
        }
        final KeyedSnapshotEntry that = (KeyedSnapshotEntry) other;
        return seen == that.seen && key.equals(that.key) && hash.equals(that.hash) && Arrays.equals(value, that.value);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(key, hash, seen, Arrays.hashCode(value));
    }
}
