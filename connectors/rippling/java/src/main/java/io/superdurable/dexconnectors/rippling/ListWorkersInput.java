// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Select one page of visible workers. */
public final class ListWorkersInput {
    /** Page size up to 100. Zero uses the tenant default. */
    public final long limit;

    /** Opaque cursor from the prior page. Empty requests the first page. */
    public final String cursor;

    /** Restore the value from its Dex wire fields. */
    @JsonCreator
    public ListWorkersInput(
            @JsonProperty("limit") long limit, @JsonProperty("cursor") String cursor) {
        this.limit = limit;
        this.cursor = cursor == null ? "" : cursor;
    }
}
