// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.*;
import org.junit.jupiter.api.io.TempDir;

import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

@Tag("dex-dev")
class WorkerDirectoryFlowTest {

    @TempDir Path directory;

    @Test
    void retryLookup() throws Exception {
        Fixture fixture = new Fixture(true);
        WorkerSummary result =
                read(
                        new GetWorkerFlow(fixture.client.getWorker()),
                        new GetWorkerInput("worker-123"),
                        WorkerSummary.class);
        assertEquals("worker-123", result.id);
        fixture.verify();
    }

    @Test
    void retryDirectory() throws Exception {
        Fixture fixture = new Fixture(false);
        WorkerPage page =
                read(
                        new ListWorkersFlow(fixture.client.listWorkers()),
                        new ListWorkersInput(2, "a+=&"),
                        WorkerPage.class);
        assertEquals("worker-123", page.workers.get(0).id);
        assertEquals("later+/=", page.nextCursor);
        assertEquals("Engineer", page.workers.get(0).title);
        fixture.verify();
    }

    private <I, O> O read(Flow<I> flow, I input, Class<O> output) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        directory.resolve("blobs").toString(), 16L << 20));
                Client client = new Client(registry, cache, new ClientOptions(address))) {
            FutureTask<Void> serving;
            O value;
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
                new Thread(serving, "rippling-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "rippling-" + UUID.randomUUID();
                client.startFlow(flow, flowId, input);
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                value = result.getSingleOutput(output);
            }
            serving.get(30, TimeUnit.SECONDS);
            return value;
        }
    }

    static final class Fixture {
        final AtomicInteger reads = new AtomicInteger();
        final RipplingClient client;

        Fixture(boolean lookup) {
            client =
                    new RipplingClient(
                            call -> new SecretString("test-bearer"),
                            "https://rest.ripplingapis.com",
                            request -> {
                                assertEquals("GET", request.getMethod());
                                assertEquals(
                                        "Bearer test-bearer",
                                        request.getHeaders().get("Authorization"));
                                assertEquals(
                                        "https://rest.ripplingapis.com/workers/"
                                                + (lookup
                                                        ? "worker-123/"
                                                        : "?cursor=a%2B%3D%26&limit=2"),
                                        request.getUrl());
                                String worker =
                                        "{\"id\":\"worker-123\",\"title\":\"Engineer\",\"status\":null,\"privateField\":42}";
                                String body =
                                        lookup
                                                ? worker
                                                : "{\"results\":["
                                                        + worker
                                                        + "],\"next_link\":\"?cursor=later%2B%2F%3D\"}";
                                return ProviderHttpResponse.buffered(
                                        reads.incrementAndGet() == 1 ? 503 : 200,
                                        ProviderHttpHeaders.EMPTY,
                                        body.getBytes(StandardCharsets.UTF_8));
                            },
                            1 << 20);
        }

        void verify() {
            assertEquals(2, reads.get());
        }
    }
}
