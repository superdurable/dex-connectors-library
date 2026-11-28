// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitcode;

import com.fasterxml.jackson.annotation.*;

/** Selected GitCode repository metadata. */
public final class GetRepositoryOutput {
    /** Stable numeric repository ID. */
    public final long id;

    /** Owner and repository path. */
    public final String fullName;

    /** Whether the repository is private. */
    @JsonProperty("private")
    public final boolean isPrivate;

    /** Default branch, empty when omitted. */
    public final String defaultBranch;

    /** Construct the GetRepositoryOutput value from its wire fields. */
    @JsonCreator
    public GetRepositoryOutput(
            @JsonProperty("id") long id,
            @JsonProperty("fullName") String fullName,
            @JsonProperty("private") boolean isPrivate,
            @JsonProperty("defaultBranch") String defaultBranch) {
        this.id = id;
        this.fullName = fullName == null ? "" : fullName;
        this.isPrivate = isPrivate;
        this.defaultBranch = defaultBranch == null ? "" : defaultBranch;
    }
}
