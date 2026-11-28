// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

/** Only the ID and names of one visible person. */
public final class PersonProfile {
    /** Stable person identifier. */
    public final String id;

    /** First name or empty. */
    public final String firstName;

    /** Last name or empty. */
    public final String lastName;

    /** Construct the PersonProfile wire value. Omitted text fields default to empty. */
    @JsonCreator
    public PersonProfile(
            @JsonProperty("id") String id,
            @JsonProperty("firstName") String firstName,
            @JsonProperty("lastName") String lastName) {
        this.id = id == null ? "" : id;
        this.firstName = firstName == null ? "" : firstName;
        this.lastName = lastName == null ? "" : lastName;
    }
}
