// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiling;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** Accessible department directory in provider order. */
public final class ListDepartmentsOutput {
    /** Department identities, display names and parents. */
    public final List<DepartmentSummary> departments;

    /** Restore the directory from a Dex result. */
    @JsonCreator
    public ListDepartmentsOutput(@JsonProperty("departments") List<DepartmentSummary> departments) {
        this.departments =
                java.util.Collections.unmodifiableList(new java.util.ArrayList<>(departments));
    }

    /** Public metadata for one department. */
    public static final class DepartmentSummary {
        /** Department identifier. */
        public final String id;

        /** Department display name. */
        public final String name;

        /** Parent department ID, or empty for a top-level department. */
        public final String parentID;

        /** Restore one department from a Dex result. */
        @JsonCreator
        public DepartmentSummary(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("parentID") String parentID) {
            this.id = id;
            this.name = name;
            this.parentID = parentID;
        }
    }
}
