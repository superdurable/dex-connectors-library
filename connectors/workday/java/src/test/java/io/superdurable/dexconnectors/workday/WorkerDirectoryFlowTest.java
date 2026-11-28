// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dex.*;
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
    void refreshAndRetryLookup() throws Exception {
        Fixture fixture = new Fixture(true);
        WorkerSummary result =
                read(
                        new GetWorkerFlow(fixture.client.getWorker()),
                        new GetWorkerInput("0123456789abcdef0123456789abcdef"),
                        WorkerSummary.class);
        assertEquals("0123456789abcdef0123456789abcdef", result.id);
        fixture.verify();
    }

    @Test
    void refreshAndRetryDirectory() throws Exception {
        Fixture fixture = new Fixture(false);
        WorkerPage page =
                read(
                        new ListWorkersFlow(fixture.client.listWorkers()),
                        new ListWorkersInput(0, 0, ""),
                        WorkerPage.class);
        assertEquals("0123456789abcdef0123456789abcdef", page.data.get(0).id);
        assertEquals(7, page.total);
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
                new Thread(serving, "workday-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "workday-" + UUID.randomUUID();
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
        final AtomicInteger exchanges = new AtomicInteger(), reads = new AtomicInteger();
        final WorkdayClient client;
        final LoadedProject project;

        Fixture(boolean lookup) throws Exception {
            project = WorkdayTestProject.exampleProject("test-id", "test-secret");
            WorkdayClient.fromProject(project, "workday");
            client =
                    new WorkdayClient(
                            WorkdayCredentials.fromProject(project, "workday"),
                            "https://tenant.example.com/api/common/v1/demo",
                            "https://tenant.example.com/token",
                            request -> {
                                int status = 200;
                                String body;
                                if (request.getUrl().endsWith("/token")) {
                                    assertEquals("POST", request.getMethod());
                                    assertEquals(
                                            "Basic dGVzdC1pZDp0ZXN0LXNlY3JldA==",
                                            request.getHeaders().get("Authorization"));
                                    String form =
                                            new String(request.getBody(), StandardCharsets.UTF_8);
                                    assertTrue(form.contains("grant_type=refresh_token"));
                                    assertTrue(form.contains("refresh_token=test-refresh"));
                                    exchanges.incrementAndGet();
                                    body =
                                            "{\"access_token\":\"test-bearer\",\"refresh_token\":\"rotated\",\"expires_in\":3600,\"token_type\":\"Bearer\"}";
                                } else {
                                    assertEquals("GET", request.getMethod());
                                    assertEquals(
                                            "Bearer test-bearer",
                                            request.getHeaders().get("Authorization"));
                                    assertEquals(
                                            "https://tenant.example.com/api/common/v1/demo/workers"
                                                    + (lookup
                                                            ? "/0123456789abcdef0123456789abcdef"
                                                            : ""),
                                            request.getUrl());
                                    if (reads.incrementAndGet() == 1) status = 503;
                                    String profile =
                                            "{\"id\":\"0123456789abcdef0123456789abcdef\",\"descriptor\":\"Test\",\"privateField\":42}";
                                    body =
                                            lookup
                                                    ? profile
                                                    : "{\"data\":[" + profile + "],\"total\":7}";
                                }
                                return ProviderHttpResponse.buffered(
                                        status,
                                        ProviderHttpHeaders.EMPTY,
                                        body.getBytes(StandardCharsets.UTF_8));
                            },
                            1 << 20);
        }

        void verify() throws Exception {
            assertEquals(1, exchanges.get());
            assertEquals(2, reads.get());
            CredentialMaterial stored =
                    project.getConnections()
                            .readCredentialMaterial(new ConnectionKey("workday", "workday"))
                            .getMaterial();
            assertNotNull(stored.getExpiresAt());
            assertEquals("rotated", stored.getCredentials().path("refresh_token").asText());
            assertEquals("test-bearer", stored.getCredentials().path("access_token").asText());
            assertEquals(
                    "test-secret", stored.getCredentials().path("oauth_client_secret").asText());
        }
    }
}
