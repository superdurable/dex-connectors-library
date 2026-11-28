// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.moka;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Query result in provider order. */
public final class ListJobFunctionsOutput {
    /** Functions returned by the provider. */
    public final List<JobFunction> functions;

    /** Restore the value from Dex wire fields. */
    @JsonCreator
    public ListJobFunctionsOutput(@JsonProperty("functions") List<JobFunction> functions) {
        this.functions =
                java.util.Collections.unmodifiableList(new java.util.ArrayList<>(functions));
    }

    /** Metadata for one job function. */
    public static final class JobFunction {
        /** Job function identifier. */
        public final long id;

        /** Job function label. */
        public final String label;

        /** Parent function identifier, or zero for a root. */
        public final long parentId;

        /** Number of jobs in the function. */
        public final long jobCount;

        /** Restore the value from Dex wire fields. */
        @JsonCreator
        public JobFunction(
                @JsonProperty("id") long id,
                @JsonProperty("label") String label,
                @JsonProperty("parentId") long parentId,
                @JsonProperty("jobCount") long jobCount) {
            this.id = id;
            this.label = label;
            this.parentId = parentId;
            this.jobCount = jobCount;
        }
    }
}
