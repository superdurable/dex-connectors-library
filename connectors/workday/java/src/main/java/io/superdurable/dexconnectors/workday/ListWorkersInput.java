// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Select one page of visible workers. */
public final class ListWorkersInput {
    /** Page size up to 100. Zero uses the tenant default. */
    public final long limit;

    /** Zero-based index of the first worker. */
    public final long offset;

    /** Optional name prefix of at least three UTF-8 bytes after trimming. */
    public final String search;

    /** Restore the value from its Dex wire fields. */
    @JsonCreator
    public ListWorkersInput(
            @JsonProperty("limit") long limit,
            @JsonProperty("offset") long offset,
            @JsonProperty("search") String search) {
        this.limit = limit;
        this.offset = offset;
        this.search = search == null ? "" : search;
    }
}
