// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.huoban;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.huoban.SpaceDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the space lookup Flow with the static huoban connection. */
public final class SpaceDirectoryWorker {
    private SpaceDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("HUOBAN_API_KEY"), "HUOBAN_API_KEY is required"));
        SpaceDirectoryFlow flow = new SpaceDirectoryFlow(new ListSpacesOperation(call -> token));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("space-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its space catalog. */
    public static final class SpaceDirectoryFlow implements Flow<ListSpacesInput> {
        private final QueryStep<
                        ListSpacesInput, ListSpacesInput, ListSpacesOutput, ListSpacesResult>
                query;
        private final ReturnSpaces complete = new ReturnSpaces();

        /** Bind the operation during Worker registration. */
        public SpaceDirectoryFlow(ListSpacesOperation operation) {
            query =
                    new QueryStep<
                            ListSpacesInput, ListSpacesInput, ListSpacesOutput, ListSpacesResult>(
                            SpaceDirectory.newListSpacesStep(
                                            "ListSpaces",
                                            ListSpacesInput.class,
                                            new StepAnnotations(
                                                    "space-directory",
                                                    "Space catalog",
                                                    "Read the available space catalog."),
                                            operation,
                                            new ConnectionRef("huoban", "huoban"),
                                            input -> input)
                                    .branch("listed", ReturnSpaces.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaHuobanSpaceDirectory";
        }

        @Override
        public StepList<ListSpacesInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:space-directory group-label:"Space catalog"
    // dex:explanation text:"Complete with the provider's visible space catalog."
    public static final class ReturnSpaces implements Step<ListSpacesResult> {
        @Override
        public Class<ListSpacesResult> getInputType() {
            return ListSpacesResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListSpacesResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
