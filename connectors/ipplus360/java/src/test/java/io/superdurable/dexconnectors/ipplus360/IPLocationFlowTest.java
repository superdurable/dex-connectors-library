// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ipplus360;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.ipplus360.IPLocation.LookupIPLocationInput;
import io.superdurable.dexconnectors.ipplus360.IPLocation.LookupIPLocationOperation;
import io.superdurable.dexconnectors.ipplus360.IPLocation.LookupIPLocationOutput;
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
class IPLocationFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        LookupIPLocationOperation operation =
                new LookupIPLocationOperation(
                        call -> new SecretString("test-key"),
                        request ->
                                ProviderHttpResponse.buffered(
                                        calls.incrementAndGet() == 1 ? 503 : 200,
                                        ProviderHttpHeaders.EMPTY,
                                        "{\"code\":\"Success\",\"data\":{\"country\":\"US\",\"city\":\"Mountain View\",\"district\":\"North\"}}"
                                                .getBytes(StandardCharsets.UTF_8)),
                        1 << 20);
        IPLocationWorker.LocationFlow flow = new IPLocationWorker.LocationFlow(operation);
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
                new Thread(serving, "ip-location-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-ip-location-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new LookupIPLocationInput("8.8.8.8"));
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                assertEquals(
                        "Mountain View", result.getSingleOutput(LookupIPLocationOutput.class).city);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
