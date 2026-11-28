// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.moka;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.moka.JobFunctionDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the job function lookup Flow with the static moka connection. */
public final class JobFunctionDirectoryWorker {
    private JobFunctionDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("MOKA_API_KEY"), "MOKA_API_KEY is required"));
        JobFunctionDirectoryFlow flow =
                new JobFunctionDirectoryFlow(
                        new ListJobFunctionsOperation(
                                call -> token,
                                System.getenv("MOKA_ORG_ID"),
                                System.getenv("MOKA_MODE")));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("job-function-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its job function catalog. */
    public static final class JobFunctionDirectoryFlow implements Flow<ListJobFunctionsInput> {
        private final QueryStep<
                        ListJobFunctionsInput,
                        ListJobFunctionsInput,
                        ListJobFunctionsOutput,
                        ListJobFunctionsResult>
                query;
        private final ReturnFunctions complete = new ReturnFunctions();

        /** Bind the operation during Worker registration. */
        public JobFunctionDirectoryFlow(ListJobFunctionsOperation operation) {
            query =
                    new QueryStep<
                            ListJobFunctionsInput,
                            ListJobFunctionsInput,
                            ListJobFunctionsOutput,
                            ListJobFunctionsResult>(
                            JobFunctionDirectory.newListJobFunctionsStep(
                                            "ListJobFunctions",
                                            ListJobFunctionsInput.class,
                                            new StepAnnotations(
                                                    "job-function-directory",
                                                    "JobFunction catalog",
                                                    "Read the available job function catalog."),
                                            operation,
                                            new ConnectionRef("moka", "moka"),
                                            input -> input)
                                    .branch("found", ReturnFunctions.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaMokaJobFunctionDirectory";
        }

        @Override
        public StepList<ListJobFunctionsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:job-function-directory group-label:"JobFunction catalog"
    // dex:explanation text:"Complete with the provider's visible job function catalog."
    public static final class ReturnFunctions implements Step<ListJobFunctionsResult> {
        @Override
        public Class<ListJobFunctionsResult> getInputType() {
            return ListJobFunctionsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListJobFunctionsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
