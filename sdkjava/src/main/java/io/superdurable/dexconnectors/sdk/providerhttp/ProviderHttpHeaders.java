// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;

/**
 * Case-insensitive, immutable provider response headers.
 *
 * <p>{@link #toString()} lists only header names, never values.
 */
public final class ProviderHttpHeaders {
    /** Headers without entries. */
    public static final ProviderHttpHeaders EMPTY = new ProviderHttpHeaders(Collections.<String, List<String>>emptyMap());

    private final Map<String, List<String>> values;

    /**
     * Copies headers, ignoring a {@code null} name such as the HTTP status line.
     *
     * @param headers the header values by name
     */
    public ProviderHttpHeaders(final Map<String, List<String>> headers) {
        final TreeMap<String, List<String>> copy = new TreeMap<>(String.CASE_INSENSITIVE_ORDER);
        for (Map.Entry<String, List<String>> entry : headers.entrySet()) {
            if (entry.getKey() == null || entry.getValue() == null) {
                continue;
            }
            final List<String> existing = copy.get(entry.getKey());
            final List<String> merged = existing == null ? new ArrayList<String>() : new ArrayList<>(existing);
            merged.addAll(entry.getValue());
            copy.put(entry.getKey(), Collections.unmodifiableList(merged));
        }
        this.values = Collections.unmodifiableMap(copy);
    }

    /**
     * Returns the first value of a header.
     *
     * @param name the case-insensitive header name
     * @return the value, or {@code null} when absent
     */
    public String first(final String name) {
        final List<String> entries = values.get(name);
        return entries == null || entries.isEmpty() ? null : entries.get(0);
    }

    /**
     * Returns every value of a header.
     *
     * @param name the case-insensitive header name
     * @return the unmodifiable values, empty when absent
     */
    public List<String> all(final String name) {
        final List<String> entries = values.get(name);
        return entries == null ? Collections.<String>emptyList() : entries;
    }

    /**
     * Returns every header.
     *
     * @return the unmodifiable case-insensitive map
     */
    public Map<String, List<String>> asMap() {
        return values;
    }

    /**
     * Returns the header names without values.
     *
     * @return the redacted description
     */
    @Override
    public String toString() {
        return "ProviderHttpHeaders" + values.keySet();
    }
}
