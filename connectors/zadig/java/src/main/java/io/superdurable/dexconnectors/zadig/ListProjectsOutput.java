// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zadig;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** A page of authorized projects. */
public final class ListProjectsOutput {
    /** Projects returned in provider order. */
    public final List<Project> projects;

    /** Total projects reported by Zadig. */
    public final long total;

    /** Restore a page from a Dex result. */
    @JsonCreator
    public ListProjectsOutput(
            @JsonProperty("projects") List<Project> projects, @JsonProperty("total") long total) {
        this.projects = java.util.Collections.unmodifiableList(new java.util.ArrayList<>(projects));
        this.total = total;
    }

    /** Metadata for one project visible to the connection. */
    public static final class Project {
        /** Project key. */
        public final String key;

        /** Project display name. */
        public final String name;

        /** Deployment type, or empty when omitted. */
        public final String deployType;

        /** Restore project metadata from a Dex result. */
        @JsonCreator
        public Project(
                @JsonProperty("key") String key,
                @JsonProperty("name") String name,
                @JsonProperty("deployType") String deployType) {
            this.key = key;
            this.name = name;
            this.deployType = deployType;
        }
    }
}
