// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zilliz;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.zilliz.ClusterDirectory.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the cluster lookup Flow with the static zilliz connection. */
public final class ClusterDirectoryWorker {
    private ClusterDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("ZILLIZ_API_KEY"), "ZILLIZ_API_KEY is required"));
        ClusterDirectoryFlow flow =
                new ClusterDirectoryFlow(new ListClustersOperation(call -> token));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("cluster-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its cluster catalog. */
    public static final class ClusterDirectoryFlow implements Flow<ListClustersInput> {
        private final QueryStep<
                        ListClustersInput,
                        ListClustersInput,
                        ListClustersOutput,
                        ListClustersResult>
                query;
        private final ReturnClusters complete = new ReturnClusters();

        /** Bind the operation during Worker registration. */
        public ClusterDirectoryFlow(ListClustersOperation operation) {
            query =
                    new QueryStep<
                            ListClustersInput,
                            ListClustersInput,
                            ListClustersOutput,
                            ListClustersResult>(
                            ClusterDirectory.newListClustersStep(
                                            "ListClusters",
                                            ListClustersInput.class,
                                            new StepAnnotations(
                                                    "cluster-directory",
                                                    "Cluster catalog",
                                                    "Read the available cluster catalog."),
                                            operation,
                                            new ConnectionRef("zilliz", "zilliz"),
                                            input -> input)
                                    .branch("found", ReturnClusters.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaZillizClusterDirectory";
        }

        @Override
        public StepList<ListClustersInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:cluster-directory group-label:"Cluster catalog"
    // dex:explanation text:"Complete with the provider's visible cluster catalog."
    public static final class ReturnClusters implements Step<ListClustersResult> {
        @Override
        public Class<ListClustersResult> getInputType() {
            return ListClustersResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListClustersResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
