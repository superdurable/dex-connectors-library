// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qingflow;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.qingflow.ApplicationDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the application lookup Flow with the static qingflow connection. */
public final class ApplicationDirectoryWorker {
    private ApplicationDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("QINGFLOW_ACCESS_TOKEN"),
                                "QINGFLOW_ACCESS_TOKEN is required"));
        ApplicationDirectoryFlow flow =
                new ApplicationDirectoryFlow(new ListApplicationsOperation(call -> token));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("application-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its application catalog. */
    public static final class ApplicationDirectoryFlow implements Flow<ListApplicationsInput> {
        private final QueryStep<
                        ListApplicationsInput,
                        ListApplicationsInput,
                        ListApplicationsOutput,
                        ListApplicationsResult>
                query;
        private final ReturnApplications complete = new ReturnApplications();

        /** Bind the operation during Worker registration. */
        public ApplicationDirectoryFlow(ListApplicationsOperation operation) {
            query =
                    new QueryStep<
                            ListApplicationsInput,
                            ListApplicationsInput,
                            ListApplicationsOutput,
                            ListApplicationsResult>(
                            ApplicationDirectory.newListApplicationsStep(
                                            "ListApplications",
                                            ListApplicationsInput.class,
                                            new StepAnnotations(
                                                    "application-directory",
                                                    "Application catalog",
                                                    "Read the available application catalog."),
                                            operation,
                                            new ConnectionRef("qingflow", "qingflow"),
                                            input -> input)
                                    .branch("listed", ReturnApplications.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaQingflowApplicationDirectory";
        }

        @Override
        public StepList<ListApplicationsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:application-directory group-label:"Application catalog"
    // dex:explanation text:"Complete with the provider's visible application catalog."
    public static final class ReturnApplications implements Step<ListApplicationsResult> {
        @Override
        public Class<ListApplicationsResult> getInputType() {
            return ListApplicationsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListApplicationsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
