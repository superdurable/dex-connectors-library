// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitee;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.gitee.RepositoryLookup.*;
import io.superdurable.dexconnectors.sdk.*;

/** Read the selected repository. */
public final class GetRepositoryFlow implements Flow<GetRepositoryInput> {
    private final QueryStep<
                    GetRepositoryInput,
                    GetRepositoryInput,
                    GetRepositoryOutput,
                    GetRepositoryResult>
            query;
    private final ReturnRepository complete = new ReturnRepository();

    /** Bind the Gitee query when the Worker registers this Flow. */
    public GetRepositoryFlow(Query<GetRepositoryInput, GetRepositoryOutput> operation) {
        query =
                new QueryStep<
                        GetRepositoryInput,
                        GetRepositoryInput,
                        GetRepositoryOutput,
                        GetRepositoryResult>(
                        RepositoryLookup.newGetRepositoryStep(
                                        "GetRepository",
                                        GetRepositoryInput.class,
                                        new StepAnnotations(
                                                "repositories",
                                                "Repository lookup",
                                                "Read the selected repository."),
                                        operation,
                                        new ConnectionRef("gitee", "gitee"),
                                        input -> input)
                                .branch("found", ReturnRepository.class)
                                .build()) {};
    }

    @Override
    public String getFlowType() {
        return "JavaGiteeGetRepository";
    }

    @Override
    public StepList<GetRepositoryInput> getSteps() {
        return StepList.startStep(query).otherSteps(complete);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of();
    }

    // dex:group group-id:repositories group-label:"Repository lookup"
    // dex:explanation text:"Return the selected repository metadata."
    public static final class ReturnRepository implements Step<GetRepositoryResult> {
        @Override
        public Class<GetRepositoryResult> getInputType() {
            return GetRepositoryResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetRepositoryResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
