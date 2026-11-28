// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.jingdigital;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.jingdigital.TagDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the tag lookup Flow with the static jingdigital connection. */
public final class TagDirectoryWorker {
    private TagDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        TagDirectory.Credentials keys =
                new TagDirectory.Credentials(
                        new SecretString(
                                java.util.Objects.requireNonNull(
                                        System.getenv("JINGDIGITAL_APP_ID"),
                                        "JINGDIGITAL_APP_ID is required")),
                        new SecretString(
                                java.util.Objects.requireNonNull(
                                        System.getenv("JINGDIGITAL_APP_SECRET"),
                                        "JINGDIGITAL_APP_SECRET is required")));
        TagDirectoryFlow flow = new TagDirectoryFlow(new ListTagsOperation(call -> keys));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("tag-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its tag catalog. */
    public static final class TagDirectoryFlow implements Flow<ListTagsInput> {
        private final QueryStep<ListTagsInput, ListTagsInput, ListTagsOutput, ListTagsResult> query;
        private final ReturnTags complete = new ReturnTags();

        /** Bind the operation during Worker registration. */
        public TagDirectoryFlow(ListTagsOperation operation) {
            query =
                    new QueryStep<ListTagsInput, ListTagsInput, ListTagsOutput, ListTagsResult>(
                            TagDirectory.newListTagsStep(
                                            "ListTags",
                                            ListTagsInput.class,
                                            new StepAnnotations(
                                                    "tag-directory",
                                                    "Tag catalog",
                                                    "Read the available tag catalog."),
                                            operation,
                                            new ConnectionRef("jingdigital", "jingdigital"),
                                            input -> input)
                                    .branch("listed", ReturnTags.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaJINGdigitalTagDirectory";
        }

        @Override
        public StepList<ListTagsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:tag-directory group-label:"Tag catalog"
    // dex:explanation text:"Complete with the provider's visible tag catalog."
    public static final class ReturnTags implements Step<ListTagsResult> {
        @Override
        public Class<ListTagsResult> getInputType() {
            return ListTagsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListTagsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
