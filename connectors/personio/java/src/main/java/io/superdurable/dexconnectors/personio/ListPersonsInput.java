// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Filters and cursor for one directory page. */
public final class ListPersonsInput {
    /** Page size from 1 to 50. Zero requests the default. */
    public final long limit;

    /** Opaque cursor. Empty requests the first page. */
    public final String cursor;

    /** Exact first name, or empty for all. */
    public final String firstName;

    /** Exact last name, or empty for all. */
    public final String lastName;

    /** ACTIVE or INACTIVE. Empty includes both. */
    public final String status;

    /** Construct the ListPersonsInput wire value. Omitted text fields default to empty. */
    @JsonCreator
    public ListPersonsInput(
            @JsonProperty("limit") long limit,
            @JsonProperty("cursor") String cursor,
            @JsonProperty("firstName") String firstName,
            @JsonProperty("lastName") String lastName,
            @JsonProperty("status") String status) {
        this.limit = limit;
        this.cursor = cursor == null ? "" : cursor;
        this.firstName = firstName == null ? "" : firstName;
        this.lastName = lastName == null ? "" : lastName;
        this.status = status == null ? "" : status;
    }
}
