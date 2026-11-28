// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitcode;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.sun.net.httpserver.HttpServer;

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
class RepositoryFlowTest {

    @TempDir Path directory;

    @Test
    void readsRepositoryAfterRetry() throws Exception {
        AtomicInteger calls = new AtomicInteger();
        HttpServer server = HttpServer.create(new java.net.InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext(
                "/api/v5/repos/example/sample",
                exchange -> {
                    try {
                        boolean valid =
                                "GET".equals(exchange.getRequestMethod())
                                        && "application/json"
                                                .equals(
                                                        exchange.getRequestHeaders()
                                                                .getFirst("Accept"))
                                        && exchange.getRequestURI().getRawQuery() == null
                                        && "Bearer test+token"
                                                .equals(
                                                        exchange.getRequestHeaders()
                                                                .getFirst("Authorization"));
                        byte[] body =
                                "{\"id\":123,\"full_name\":\"example/sample\",\"private\":true,\"name\":\"sample\",\"owner\":{\"email\":\"excluded@example.org\"}}"
                                        .getBytes(StandardCharsets.UTF_8);
                        int status = valid ? (calls.incrementAndGet() == 1 ? 503 : 200) : 403;
                        exchange.sendResponseHeaders(status, body.length);
                        exchange.getResponseBody().write(body);
                    } finally {
                        exchange.close();
                    }
                });
        server.start();
        try {
            ObjectMapper json = new ObjectMapper();
            ObjectNode settings =
                    json.createObjectNode()
                            .put("baseUrl", "http://127.0.0.1:" + server.getAddress().getPort())
                            .put("timeout", 4000000000L)
                            .put("maxResponseBytes", 2048);
            LoadedProject project = RepositoryWorker.exampleProject("test+token", settings);
            GetRepositoryFlow flow =
                    new GetRepositoryFlow(GetRepositoryOperation.fromProject(project, "gitcode"));
            GetRepositoryOutput output =
                    read(
                            flow,
                            new GetRepositoryInput("example", "sample"),
                            GetRepositoryOutput.class);
            assertEquals("example/sample", output.fullName);
            assertFalse(json.writeValueAsString(output).contains("excluded@example.org"));
            assertEquals(2, calls.get());
        } finally {
            server.stop(0);
        }
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
                new Thread(serving, "gitcode-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "gitcode-" + UUID.randomUUID();
                client.startFlow(flow, flowId, input);
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                value = result.getSingleOutput(output);
            }
            serving.get(30, TimeUnit.SECONDS);
            return value;
        }
    }
}
