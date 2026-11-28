// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.stepfun;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.stepfun.ModelCatalog.ListModelsOperation;

import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.time.Duration;
import java.util.Collections;
import java.util.UUID;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import static org.junit.jupiter.api.Assertions.assertEquals;

@Tag("dex-dev")
class ModelCatalogFlowTest {
    @Test void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListModelsOperation operation = new ListModelsOperation(call -> new SecretString("secret-api-key"), request ->
                ProviderHttpResponse.buffered(calls.incrementAndGet() == 1 ? 503 : 200, ProviderHttpHeaders.EMPTY,
                        "{\"object\":\"list\",\"data\":[{\"id\":\"example\",\"object\":\"model\",\"created\":1711111111,\"owned_by\":\"stepfun\"}]}".getBytes(StandardCharsets.UTF_8)), 1 << 20);
        ModelCatalogWorker.CatalogFlow flow = new ModelCatalogWorker.CatalogFlow(operation);
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache = BlobCache.open(new BlobCacheConfig(directory.toString(), 16L << 20));
             Client client = new Client(registry, cache, new ClientOptions(address))) {
            FutureTask<Void> serving;
            try (Worker worker = new Worker(registry, cache, WorkerOptions.newBuilder().serverAddress(address).build())) {
                serving = new FutureTask<>(() -> { worker.start(); return null; });
                new Thread(serving, "model-catalog-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-model-catalog-" + UUID.randomUUID();
                client.startFlow(flow, flowId, "");
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                assertEquals("example", result.getSingleOutput(ListModelsOutput.class).models.get(0).id);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
