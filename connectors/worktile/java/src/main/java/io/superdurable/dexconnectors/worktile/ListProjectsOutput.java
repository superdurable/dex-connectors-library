// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.worktile;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** A page of authorized projects. */
public final class ListProjectsOutput {
    /** Projects returned in provider order. */
    public final List<Project> projects;

    /** Zero-based page index. */
    public final long pageIndex;

    /** Page size reported by Worktile. */
    public final long pageSize;

    /** Whether another page is available. */
    public final boolean hasMore;

    /** Restore a page from a Dex result. */
    @JsonCreator
    public ListProjectsOutput(
            @JsonProperty("projects") List<Project> projects,
            @JsonProperty("pageIndex") long pageIndex,
            @JsonProperty("pageSize") long pageSize,
            @JsonProperty("hasMore") boolean hasMore) {
        this.projects = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(projects));
        this.pageIndex = pageIndex;
        this.pageSize = pageSize;
        this.hasMore = hasMore;
    }

    /** Metadata for one project visible to the connection. */
    public static final class Project {
        /** Project identifier. */
        public final String id;

        /** Project display name. */
        public final String name;

        /** Project code, or empty when omitted. */
        public final String identifier;

        /** One for a public project and two for a private project. */
        public final long visibility;

        /** Restore project metadata from a Dex result. */
        @JsonCreator
        public Project(
                @JsonProperty("id") String id,
                @JsonProperty("name") String name,
                @JsonProperty("identifier") String identifier,
                @JsonProperty("visibility") long visibility) {
            this.id = id;
            this.name = name;
            this.identifier = identifier;
            this.visibility = visibility;
        }
    }
}
