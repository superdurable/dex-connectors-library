// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.baichuan;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.baichuan.KnowledgeBaseDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the knowledge-base lookup Flow with the static baichuan connection. */
public final class KnowledgeBaseWorker {
    private KnowledgeBaseWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("BAICHUAN_API_KEY");
        if (key == null || key.isEmpty())
            throw new IllegalStateException("BAICHUAN_API_KEY is required");
        KnowledgeBaseFlow flow =
                new KnowledgeBaseFlow(
                        new ListKnowledgeBasesOperation(call -> new SecretString(key)));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("knowledge-base-blobs")
                                                .toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its knowledge base directory. */
    public static final class KnowledgeBaseFlow implements Flow<String> {
        private final QueryStep<
                        String,
                        ListKnowledgeBasesInput,
                        ListKnowledgeBasesOutput,
                        ListKnowledgeBasesResult>
                query;
        private final ReturnKnowledgeBases complete = new ReturnKnowledgeBases();

        /** Bind the operation during Worker registration. */
        public KnowledgeBaseFlow(ListKnowledgeBasesOperation operation) {
            query =
                    new QueryStep<
                            String,
                            ListKnowledgeBasesInput,
                            ListKnowledgeBasesOutput,
                            ListKnowledgeBasesResult>(
                            KnowledgeBaseDirectory.newListKnowledgeBasesStep(
                                            "ListKnowledgeBases",
                                            String.class,
                                            new StepAnnotations(
                                                    "knowledge-base-directory",
                                                    "Knowledge bases",
                                                    "Read the available knowledge bases."),
                                            operation,
                                            new ConnectionRef("baichuan", "baichuan"),
                                            ignored -> new ListKnowledgeBasesInput())
                                    .branch("listed", ReturnKnowledgeBases.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaBaichuanKnowledgeBaseDirectory";
        }

        @Override
        public StepList<String> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:knowledge-base-directory group-label:"Knowledge bases"
    // dex:explanation text:"Complete with the provider's visible knowledge bases."
    public static final class ReturnKnowledgeBases implements Step<ListKnowledgeBasesResult> {
        @Override
        public Class<ListKnowledgeBasesResult> getInputType() {
            return ListKnowledgeBasesResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListKnowledgeBasesResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
