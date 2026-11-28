// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitee;

import com.fasterxml.jackson.annotation.*;

/** Selected Gitee repository metadata. */
public final class RepositoryMetadata {
    /** Stable numeric repository ID. */
    public final long id;

    /** Owner and repository path. */
    public final String fullName;

    /** Whether the repository is private. */
    @JsonProperty("private")
    public final boolean isPrivate;

    /** Default branch, empty when omitted. */
    public final String defaultBranch;

    /** Repository name. */
    public final String name;

    /** Repository description, empty when omitted. */
    public final String description;

    /** Repository web page URL. */
    public final String htmlUrl;

    /** Number of stars. */
    public final long starsCount;

    /** Number of forks. */
    public final long forksCount;

    /** Construct the RepositoryMetadata value from its wire fields. */
    @JsonCreator
    public RepositoryMetadata(
            @JsonProperty("id") long id,
            @JsonProperty("fullName") String fullName,
            @JsonProperty("private") boolean isPrivate,
            @JsonProperty("defaultBranch") String defaultBranch,
            @JsonProperty("name") String name,
            @JsonProperty("description") String description,
            @JsonProperty("htmlUrl") String htmlUrl,
            @JsonProperty("starsCount") long starsCount,
            @JsonProperty("forksCount") long forksCount) {
        this.id = id;
        this.fullName = fullName == null ? "" : fullName;
        this.isPrivate = isPrivate;
        this.defaultBranch = defaultBranch == null ? "" : defaultBranch;
        this.name = name == null ? "" : name;
        this.description = description == null ? "" : description;
        this.htmlUrl = htmlUrl == null ? "" : htmlUrl;
        this.starsCount = starsCount;
        this.forksCount = forksCount;
    }
}
