// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.rippling.WorkerDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

/** Read one page of visible workers. */
public final class ListWorkersFlow implements Flow<ListWorkersInput> {
    private final QueryStep<ListWorkersInput, ListWorkersInput, WorkerPage, ListWorkersResult>
            query;
    private final ReturnWorkerPage complete = new ReturnWorkerPage();

    /** Bind the Rippling query when the Worker registers this Flow. */
    public ListWorkersFlow(Query<ListWorkersInput, WorkerPage> operation) {
        query =
                new QueryStep<ListWorkersInput, ListWorkersInput, WorkerPage, ListWorkersResult>(
                        WorkerDirectory.newListWorkersStep(
                                        "ListWorkers",
                                        ListWorkersInput.class,
                                        new StepAnnotations(
                                                "workers",
                                                "Worker directory",
                                                "Read one page of visible workers."),
                                        operation,
                                        new ConnectionRef("rippling", "rippling"),
                                        input -> input)
                                .branch("found", ReturnWorkerPage.class)
                                .build()) {};
    }

    @Override
    public String getFlowType() {
        return "JavaRipplingListWorkers";
    }

    @Override
    public StepList<ListWorkersInput> getSteps() {
        return StepList.startStep(query).otherSteps(complete);
    }

    @Override
    public PersistenceSchema getPersistenceSchema() {
        return PersistenceSchema.of();
    }

    // dex:group group-id:workers group-label:"Worker directory"
    // dex:explanation text:"Return the selected worker fields."
    public static final class ReturnWorkerPage implements Step<ListWorkersResult> {
        @Override
        public Class<ListWorkersResult> getInputType() {
            return ListWorkersResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListWorkersResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
