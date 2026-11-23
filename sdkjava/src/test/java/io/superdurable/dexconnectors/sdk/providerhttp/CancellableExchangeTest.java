// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConformanceFixtures.FakeContext;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.singledispatch.Fence;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class CancellableExchangeTest {
    @AfterEach
    void clearHook() {
        SocketHttpExchange.beforeRequestWriteForTests = null;
    }

    @Test
    void cancellationAfterTheLastCheckNeverReopensTheConnection() throws Exception {
        final AtomicInteger requests = new AtomicInteger();
        final HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/items", exchange -> {
            requests.incrementAndGet();
            exchange.sendResponseHeaders(200, -1);
            exchange.close();
        });
        server.start();
        final CountDownLatch paused = new CountDownLatch(1);
        final CountDownLatch release = new CountDownLatch(1);
        SocketHttpExchange.beforeRequestWriteForTests = () -> {
            paused.countDown();
            boolean isInterrupted = false;
            while (true) {
                try {
                    if (release.await(10, TimeUnit.SECONDS)) {
                        break;
                    }
                } catch (InterruptedException ignored) {
                    // Simulates a send thread that keeps going after the Fence interrupts it.
                    isInterrupted = true;
                }
            }
            if (isInterrupted) {
                Thread.currentThread().interrupt();
            }
        };
        try {
            final FakeContext context = new FakeContext("flow-42", "step-execution-21", 1, null);
            final Fence fence = new Fence(Call.create(CallId.of("e18c5cc6-9bd8-56c4-8557-f0cd415e65d4"),
                    new ConnectionRef("sample", "main"), new OperationRef("sample", "createItem"), context.context(), 1),
                    Duration.ofSeconds(30), 0.5, 1);
            fence.decide();
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(30), AddressPolicy.UNRESTRICTED);
            final CountDownLatch sendReturned = new CountDownLatch(1);
            final Thread handler = new Thread(() -> {
                try {
                    fence.dispatch(writeDeadline -> {
                        try {
                            return client.send(ProviderHttpRequest.newBuilder("POST",
                                    "http://127.0.0.1:" + server.getAddress().getPort() + "/items")
                                    .body("{}".getBytes(StandardCharsets.UTF_8)).build());
                        } finally {
                            sendReturned.countDown();
                        }
                    });
                } catch (InterruptedException | IOException expected) {
                    // Cancellation ends the dispatch; the provider must see nothing.
                }
            });
            handler.start();
            assertTrue(paused.await(5, TimeUnit.SECONDS));
            context.requestCancellation();
            handler.join(5_000);
            assertFalse(handler.isAlive());
            release.countDown();
            assertTrue(sendReturned.await(5, TimeUnit.SECONDS));
            assertEquals(0, requests.get());
        } finally {
            release.countDown();
            server.stop(0);
        }
    }

    @Test
    void readsChunkedAndCloseDelimitedBodiesOverTheOwnedSocket() throws Exception {
        final HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/chunked", exchange -> {
            final byte[] received = readAll(exchange.getRequestBody());
            exchange.getResponseHeaders().add("X-Echo-Length", Integer.toString(received.length));
            exchange.sendResponseHeaders(201, 0);
            try (OutputStream output = exchange.getResponseBody()) {
                output.write("first-".getBytes(StandardCharsets.UTF_8));
                output.flush();
                output.write("second".getBytes(StandardCharsets.UTF_8));
            }
        });
        server.createContext("/fixed", exchange -> {
            final byte[] payload = "fixed body".getBytes(StandardCharsets.UTF_8);
            exchange.sendResponseHeaders(404, payload.length);
            try (OutputStream output = exchange.getResponseBody()) {
                output.write(payload);
            }
        });
        server.start();
        try {
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.UNRESTRICTED);
            final String base = "http://127.0.0.1:" + server.getAddress().getPort();
            final ProviderHttpResponse chunked = client.send(ProviderHttpRequest.newBuilder("POST", base + "/chunked?a=1")
                    .header("Content-Type", "application/json").body("{\"x\":1}".getBytes(StandardCharsets.UTF_8))
                    .build());
            assertEquals(201, chunked.getStatusCode());
            assertEquals("7", chunked.getHeaders().first("x-echo-length"));
            assertArrayEquals("first-second".getBytes(StandardCharsets.UTF_8), chunked.readBoundedBody(1024));
            final ProviderHttpResponse fixed = client.send(ProviderHttpRequest.newBuilder("GET", base + "/fixed").build());
            assertEquals(404, fixed.getStatusCode());
            assertArrayEquals("fixed body".getBytes(StandardCharsets.UTF_8), fixed.readBoundedBody(1024));
        } finally {
            server.stop(0);
        }
    }

    private static byte[] readAll(final InputStream input) throws IOException {
        final ByteArrayOutputStream output = new ByteArrayOutputStream();
        final byte[] buffer = new byte[1024];
        int read;
        while ((read = input.read(buffer)) >= 0) {
            output.write(buffer, 0, read);
        }
        return output.toByteArray();
    }
}
