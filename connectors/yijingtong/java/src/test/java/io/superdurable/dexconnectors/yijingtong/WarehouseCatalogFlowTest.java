// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yijingtong;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.yijingtong.WarehouseCatalog.ListWarehousesOperation;

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
                        call -> new SecretString("sk-test-token"),
                        "http://127.0.0.1:12345/",
                        request -> {
                            assertEquals(
                                    "http://127.0.0.1:12345/hwc_api/hwc_storage_list.php",
                                    request.getUrl());
                            return ProviderHttpResponse.buffered(
                                    calls.incrementAndGet() == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"error\":\"T\",\"content\":\"ok\",\"data\":[{\"nums\":\"US01\",\"pname\":\"US Warehouse\",\"country\":\"United States\",\"is_tc\":0}]}"
                                            .getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
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
                        "US01", result.getSingleOutput(WarehousePage.class).warehouses.get(0).id);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
