// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ctyun;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.ctyun.KnowledgeBaseDirectory.*;
import io.superdurable.dexconnectors.ctyun.KnowledgeBaseDirectory.Credentials;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the knowledge-base lookup Flow with the static ctyun connection. */
public final class KnowledgeBaseWorker {
    private KnowledgeBaseWorker() {}

    private static SecretString env(String name) {
        String value = System.getenv(name);
        if (value == null || value.isEmpty())
            throw new IllegalStateException(name + " is required");
        return new SecretString(value);
    }

    /** Start a Worker with the connection credentials supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        Credentials keys =
                new Credentials(
                        env("CTYUN_ACCESS_KEY"), env("CTYUN_SECURITY_KEY"), env("CTYUN_TENANT_ID"));

        KnowledgeBaseFlow flow =
                new KnowledgeBaseFlow(new ListKnowledgeBasesOperation(call -> keys));
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
                                            new ConnectionRef("ctyun", "ctyun"),
                                            ignored -> new ListKnowledgeBasesInput())
                                    .branch("listed", ReturnKnowledgeBases.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaCTYunKnowledgeBaseDirectory";
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
