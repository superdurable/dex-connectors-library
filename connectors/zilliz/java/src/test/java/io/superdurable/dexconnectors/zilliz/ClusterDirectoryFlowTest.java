// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zilliz;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.zilliz.ClusterDirectory.ListClustersOperation;

import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.time.Duration;
import java.util.Collections;
import java.util.UUID;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

@Tag("dex-dev")
class ClusterDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListClustersOperation operation =
                new ListClustersOperation(
                        call -> new SecretString("test-key"),
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://api.cloud.zilliz.com/v2/clusters?pageSize=25&currentPage=1",
                                    request.getUrl());
                            String body =
                                    "{\"code\":0,\"data\":{\"count\":2,\"clusters\":[{\"clusterId\":\"inxx-example\",\"clusterName\":\"example-cluster\",\"regionId\":\"aws-us-west-2\",\"plan\":\"Enterprise\",\"status\":\"RUNNING\"}]}}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        ClusterDirectoryWorker.ClusterDirectoryFlow flow =
                new ClusterDirectoryWorker.ClusterDirectoryFlow(operation);
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(new BlobCacheConfig(directory.toString(), 16L << 20));
                Client client = new Client(registry, cache, new ClientOptions(address))) {
            FutureTask<Void> serving;
            try (Worker worker =
                    new Worker(
                            registry,
                            cache,
                            WorkerOptions.newBuilder().serverAddress(address).build())) {
                serving =
                        new FutureTask<>(() -> {
                            worker.start();
                            return null;
                        });
                new Thread(serving, "cluster-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-cluster-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new ClusterDirectory.ListClustersInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListClustersOutput page = result.getSingleOutput(ListClustersOutput.class);
                assertEquals("inxx-example", page.clusters.get(0).clusterID);
                assertEquals("example-cluster", page.clusters.get(0).name);
                assertEquals("Enterprise", page.clusters.get(0).plan);
                assertEquals("aws-us-west-2", page.clusters.get(0).regionID);
                assertEquals("RUNNING", page.clusters.get(0).status);
                assertEquals(2, page.total);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
