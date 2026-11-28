// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitcode;

import com.fasterxml.jackson.annotation.*;

import io.superdurable.dexconnectors.sdk.*;

import java.util.function.Function;

/** Operation-specific Dex Step factories for GitCode. */
public final class RepositoryLookup {
    private RepositoryLookup() {}

    /** Concrete Dex Step input for a GetRepository result. */
    public static final class GetRepositoryResult extends QueryResult<GetRepositoryOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetRepositoryResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") GetRepositoryOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the GetRepository query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, GetRepositoryInput, GetRepositoryOutput, GetRepositoryResult>
                    newGetRepositoryStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetRepositoryInput, GetRepositoryOutput> operation,
                            ConnectionRef connection,
                            Function<I, GetRepositoryInput> mapInput) {
        if (!"gitcode".equals(connection.getProvider())) {
            throw new IllegalArgumentException("GitCode connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetRepositoryResult::new);
    }
}
