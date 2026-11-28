// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tingyun;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Authorized applications in provider order. */
public final class ListApplicationsOutput {
    /** Applications returned in provider order. */
    public final List<ApplicationMetadata> applications;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListApplicationsOutput(
            @JsonProperty("applications") List<ApplicationMetadata> applications) {
        this.applications =
                java.util.Collections.unmodifiableList(new java.util.ArrayList<>(applications));
    }

    /** Metadata for one application. */
    public static final class ApplicationMetadata {
        /** Positive application identifier. */
        public final long id;

        /** Application display name. */
        public final String name;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public ApplicationMetadata(@JsonProperty("id") long id, @JsonProperty("name") String name) {
            this.id = id;
            this.name = name;
        }
    }
}
