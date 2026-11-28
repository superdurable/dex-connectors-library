// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qingflow;

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
        /** Application key. */
        public final String appKey;

        /** Application display name. */
        public final String appName;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public ApplicationMetadata(
                @JsonProperty("appKey") String appKey, @JsonProperty("appName") String appName) {
            this.appKey = appKey;
            this.appName = appName;
        }
    }
}
