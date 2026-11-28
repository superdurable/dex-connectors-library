// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yanwen;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.yanwen.WarehouseCatalog.ListWarehousesOperation;

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
class WarehouseCatalogFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListWarehousesOperation operation =
                new ListWarehousesOperation(
                        call ->
                                new WarehouseCatalog.Credentials(
                                        "customer-1", new SecretString("REDACTED_API_TOKEN")),
                        "http://127.0.0.1:12345/",
                        request -> {
                            assertEquals(
                                    "/api/order", java.net.URI.create(request.getUrl()).getPath());
                            return ProviderHttpResponse.buffered(
                                    calls.incrementAndGet() == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"success\":true,\"code\":\"0\",\"data\":[{\"code\":\"01\",\"name\":\"Beijing Yanwen\",\"area\":\"North China\"}]}"
                                            .getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20,
                        java.time.Clock.systemUTC());
        WarehouseCatalogWorker.WarehouseCatalogFlow flow =
                new WarehouseCatalogWorker.WarehouseCatalogFlow(operation);
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
                new Thread(serving, "warehouse-catalog-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-warehouse-catalog-" + UUID.randomUUID();
                client.startFlow(flow, flowId, "");
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                assertEquals(
                        "01", result.getSingleOutput(WarehouseList.class).warehouses.get(0).code);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
