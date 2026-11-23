// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.Arrays;

/**
 * One versioned private project object.
 *
 * <p>The contents may hold credentials, so {@link #toString()} shows only the object identity and
 * the bytes are copied on access. Never serialize or log the contents.
 */
public final class ProjectObject {
    private final String key;
    private final String version;
    private final String etag;
    private final byte[] contents;

    /**
     * Creates an object value.
     *
     * @param key the object key
     * @param version the exact object version
     * @param etag the compare-and-swap token
     * @param contents the private bytes
     */
    public ProjectObject(final String key, final String version, final String etag, final byte[] contents) {
        this.key = key;
        this.version = version;
        this.etag = etag;
        this.contents = contents.clone();
    }

    /**
     * Returns the object key.
     *
     * @return the key
     */
    public String getKey() {
        return key;
    }

    /**
     * Returns the exact object version.
     *
     * @return the version
     */
    public String getVersion() {
        return version;
    }

    /**
     * Returns the compare-and-swap token to pass as the exact precondition.
     *
     * @return the ETag
     */
    public String getEtag() {
        return etag;
    }

    /**
     * Returns a copy of the private bytes.
     *
     * @return the contents
     */
    public byte[] getContents() {
        return contents.clone();
    }

    /**
     * Reports whether the contents equal the given bytes.
     *
     * @param other the bytes to compare
     * @return {@code true} when the bytes match exactly
     */
    public boolean hasContents(final byte[] other) {
        return Arrays.equals(contents, other);
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return "ProjectObject{key=" + key + ", version=" + version + "}";
    }
}
