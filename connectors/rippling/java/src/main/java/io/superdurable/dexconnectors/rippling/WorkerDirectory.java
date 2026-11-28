// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import com.fasterxml.jackson.annotation.*;

import io.superdurable.dexconnectors.sdk.*;

import java.util.function.Function;

/** Operation-specific Dex Step factories for Rippling. */
public final class WorkerDirectory {
    private WorkerDirectory() {}

    /** Concrete Dex Step input for a GetWorker result. */
    public static final class GetWorkerResult extends QueryResult<WorkerSummary> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetWorkerResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") WorkerSummary value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the GetWorker query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, GetWorkerInput, WorkerSummary, GetWorkerResult>
                    newGetWorkerStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetWorkerInput, WorkerSummary> operation,
                            ConnectionRef connection,
                            Function<I, GetWorkerInput> mapInput) {
        if (!"rippling".equals(connection.getProvider())) {
            throw new IllegalArgumentException("Rippling connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetWorkerResult::new);
    }

    /** Concrete Dex Step input for a ListWorkers result. */
    public static final class ListWorkersResult extends QueryResult<WorkerPage> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListWorkersResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") WorkerPage value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the ListWorkers query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListWorkersInput, WorkerPage, ListWorkersResult>
                    newListWorkersStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListWorkersInput, WorkerPage> operation,
                            ConnectionRef connection,
                            Function<I, ListWorkersInput> mapInput) {
        if (!"rippling".equals(connection.getProvider())) {
            throw new IllegalArgumentException("Rippling connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListWorkersResult::new);
    }
}
