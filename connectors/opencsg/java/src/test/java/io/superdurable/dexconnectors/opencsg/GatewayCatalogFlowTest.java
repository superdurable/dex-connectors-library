// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.opencsg;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.opencsg.GatewayCatalog.ListGatewayModelsOperation;
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
class GatewayCatalogFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListGatewayModelsOperation operation =
                new ListGatewayModelsOperation(
                        call -> new SecretString("test-key"),
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://ai.space.opencsg.com/v1/models", request.getUrl());
                            String body =
                                    "{\"data\":[{\"id\":\"sample-model\",\"owner\":\"private\"}]}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        GatewayCatalogWorker.GatewayCatalogFlow flow =
                new GatewayCatalogWorker.GatewayCatalogFlow(operation);
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
                new Thread(serving, "gateway-catalog-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-gateway-catalog-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new GatewayCatalog.ListGatewayModelsInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListGatewayModelsOutput page =
                        result.getSingleOutput(ListGatewayModelsOutput.class);
                assertEquals(1, page.total);
                assertEquals(Collections.singletonList("sample-model"), page.modelIDs);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
