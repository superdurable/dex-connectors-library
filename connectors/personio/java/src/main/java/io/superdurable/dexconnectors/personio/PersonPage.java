// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/** One projected directory page in provider order. */
public final class PersonPage {
    /** Persons on this page. */
    public final List<PersonProfile> persons;

    /** Opaque cursor, empty on the last page. */
    public final String nextCursor;

    /** Construct the PersonPage wire value. Omitted text fields default to empty. */
    @JsonCreator
    public PersonPage(
            @JsonProperty("persons") List<PersonProfile> persons,
            @JsonProperty("nextCursor") String nextCursor) {
        this.persons =
                persons == null
                        ? Collections.emptyList()
                        : Collections.unmodifiableList(new ArrayList<>(persons));
        this.nextCursor = nextCursor == null ? "" : nextCursor;
    }
}
