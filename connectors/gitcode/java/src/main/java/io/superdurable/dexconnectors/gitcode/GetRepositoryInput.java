// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitcode;

import com.fasterxml.jackson.annotation.*;

/** Owner and repository path segments from a GitCode repository URL. */
public final class GetRepositoryInput {
    /** Organization or user path. */
    public final String owner;

    /** Repository path. */
    public final String repo;

    /** Construct the GetRepositoryInput value from its wire fields. */
    @JsonCreator
    public GetRepositoryInput(
            @JsonProperty("owner") String owner, @JsonProperty("repo") String repo) {
        this.owner = owner == null ? "" : owner;
        this.repo = repo == null ? "" : repo;
    }
}
