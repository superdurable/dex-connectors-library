// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ctyun;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.ctyun.KnowledgeBaseDirectory.ListKnowledgeBasesOperation;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

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
class KnowledgeBaseFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListKnowledgeBasesOperation operation =
                new ListKnowledgeBasesOperation(
                        call ->
                                new KnowledgeBaseDirectory.Credentials(
                                        new SecretString("ak"),
                                        new SecretString("sk"),
                                        new SecretString("tenant")),
                        "https://kqa-global.ctapi.ctyun.cn",
                        request -> {
                            assertEquals(
                                    "https://kqa-global.ctapi.ctyun.cn/openapi/v1/infobases/list",
                                    request.getUrl());
                            return ProviderHttpResponse.buffered(
                                    calls.incrementAndGet() == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"statusCode\":200,\"returnObj\":[{\"id\":280,\"name\":\"example\",\"count\":4}]}"
                                            .getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        KnowledgeBaseWorker.KnowledgeBaseFlow flow =
                new KnowledgeBaseWorker.KnowledgeBaseFlow(operation);
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
                new Thread(serving, "knowledge-base-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-knowledge-base-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, "");
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                assertEquals(
                        280,
                        result.getSingleOutput(ListKnowledgeBasesOutput.class)
                                .knowledgeBases
                                .get(0)
                                .id);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
