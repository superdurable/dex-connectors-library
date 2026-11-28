// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zadig;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.zadig.ProjectDirectory.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the project lookup Flow with the static zadig connection. */
public final class ProjectDirectoryWorker {
    private ProjectDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("ZADIG_API_KEY"), "ZADIG_API_KEY is required"));
        String origin =
                java.util.Objects.requireNonNull(
                        System.getenv("ZADIG_BASE_URL"), "ZADIG_BASE_URL is required");
        ProjectDirectoryFlow flow =
                new ProjectDirectoryFlow(new ListProjectsOperation(call -> token, origin));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("project-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its project catalog. */
    public static final class ProjectDirectoryFlow implements Flow<ListProjectsInput> {
        private final QueryStep<
                        ListProjectsInput,
                        ListProjectsInput,
                        ListProjectsOutput,
                        ListProjectsResult>
                query;
        private final ReturnProjects complete = new ReturnProjects();

        /** Bind the operation during Worker registration. */
        public ProjectDirectoryFlow(ListProjectsOperation operation) {
            query =
                    new QueryStep<
                            ListProjectsInput,
                            ListProjectsInput,
                            ListProjectsOutput,
                            ListProjectsResult>(
                            ProjectDirectory.newListProjectsStep(
                                            "ListProjects",
                                            ListProjectsInput.class,
                                            new StepAnnotations(
                                                    "project-directory",
                                                    "Project catalog",
                                                    "Read the available project catalog."),
                                            operation,
                                            new ConnectionRef("zadig", "zadig"),
                                            input -> input)
                                    .branch("listed", ReturnProjects.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaZadigProjectDirectory";
        }

        @Override
        public StepList<ListProjectsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:project-directory group-label:"Project catalog"
    // dex:explanation text:"Complete with the provider's visible project catalog."
    public static final class ReturnProjects implements Step<ListProjectsResult> {
        @Override
        public Class<ListProjectsResult> getInputType() {
            return ListProjectsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListProjectsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
