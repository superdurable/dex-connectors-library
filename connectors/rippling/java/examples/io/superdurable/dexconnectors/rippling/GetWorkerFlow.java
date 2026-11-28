// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.rippling.WorkerDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

/** Read the selected worker. */
public final class GetWorkerFlow implements Flow<GetWorkerInput> {
    private final QueryStep<GetWorkerInput, GetWorkerInput, WorkerSummary, GetWorkerResult> query;
    private final ReturnWorkerSummary complete = new ReturnWorkerSummary();

    /** Bind the Rippling query when the Worker registers this Flow. */
    public GetWorkerFlow(Query<GetWorkerInput, WorkerSummary> operation) {
        query =
                new QueryStep<GetWorkerInput, GetWorkerInput, WorkerSummary, GetWorkerResult>(
                        WorkerDirectory.newGetWorkerStep(
                                        "GetWorker",
                                        GetWorkerInput.class,
                                        new StepAnnotations(
                                                "workers",
                                                "Worker directory",
                                                "Read the selected worker."),
                                        operation,
                                        new ConnectionRef("rippling", "rippling"),
                                        input -> input)
                                .branch("found", ReturnWorkerSummary.class)
                                .build()) {};
    }

    @Override
    public String getFlowType() {
        return "JavaRipplingGetWorker";
    }

    @Override
    public StepList<GetWorkerInput> getSteps() {
        return StepList.startStep(query).otherSteps(complete);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of();
    }

    // dex:group group-id:workers group-label:"Worker directory"
    // dex:explanation text:"Return the selected worker fields."
    public static final class ReturnWorkerSummary implements Step<GetWorkerResult> {
        @Override
        public Class<GetWorkerResult> getInputType() {
            return GetWorkerResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetWorkerResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
