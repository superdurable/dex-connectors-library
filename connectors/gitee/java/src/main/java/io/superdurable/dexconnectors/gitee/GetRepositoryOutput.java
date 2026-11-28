// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitee;

import com.fasterxml.jackson.annotation.*;

/** The selected repository. */
public final class GetRepositoryOutput {
    /** Returned metadata. */
    public final RepositoryMetadata repository;

    /** Construct the GetRepositoryOutput value from its wire fields. */
    @JsonCreator
    public GetRepositoryOutput(@JsonProperty("repository") RepositoryMetadata repository) {
        this.repository = repository;
    }
}
