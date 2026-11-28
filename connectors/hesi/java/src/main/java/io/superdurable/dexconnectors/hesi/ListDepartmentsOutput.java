// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hesi;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** One page of the authorized department directory. */
public final class ListDepartmentsOutput {
    /** Departments in provider order. */
    public final List<DepartmentSummary> departments;

    /** Total departments reported by Hesi. */
    public final long total;

    /** Next offset, or zero on the last page. */
    public final long nextStart;

    /** Restore a department page returned by Dex. */
    @JsonCreator
    public ListDepartmentsOutput(
            @JsonProperty("departments") List<DepartmentSummary> departments,
            @JsonProperty("total") long total,
            @JsonProperty("nextStart") long nextStart) {
        this.departments =
                java.util.Collections.unmodifiableList(new java.util.ArrayList<>(departments));
        this.total = total;
        this.nextStart = nextStart;
    }

    /** Public metadata for one department. */
    public static final class DepartmentSummary {
        /** Department identifier. */
        public final String id;

        /** Department display name. */
        public final String name;

        /** Provider department code, or empty when omitted. */
        public final String code;

        /** Whether the department is active. */
        public final boolean active;

        /** Restore department metadata from a Dex result. */
        @JsonCreator
        public DepartmentSummary(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("code") String code,
                @JsonProperty("active") boolean active) {
            this.id = id;
            this.name = name;
            this.code = code;
            this.active = active;
        }
    }
}
