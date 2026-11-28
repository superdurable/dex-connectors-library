// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Select one person by their stable API ID. */
public final class GetPersonInput {
    /** Personio v2 person identifier. */
    public final String personId;

    /** Construct the GetPersonInput wire value. Omitted text fields default to empty. */
    @JsonCreator
    public GetPersonInput(@JsonProperty("personId") String personId) {
        this.personId = personId == null ? "" : personId;
    }
}
