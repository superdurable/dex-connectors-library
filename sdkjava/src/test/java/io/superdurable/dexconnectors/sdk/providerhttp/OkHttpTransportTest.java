// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import com.sun.net.httpserver.HttpsConfigurator;
import com.sun.net.httpserver.HttpsServer;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConformanceFixtures.FakeContext;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.singledispatch.Fence;
import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.KeyStore;
import java.time.Duration;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import javax.net.ssl.HttpsURLConnection;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class OkHttpTransportTest {
    private static final char[] PASSWORD = "test-only-keystore".toCharArray();

    @AfterEach
    void clearHook() {
        ProviderHttpClient.beforeRequestWriteForTests = null;
    }

    @Test
    void cancellationRightBeforeTheFirstRequestByteSendsNothing() throws Exception {
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
        ProviderHttpClient.beforeRequestWriteForTests = () -> {
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
            final AtomicInteger provablyNotSent = new AtomicInteger();
            final Thread handler = new Thread(() -> {
                try {
                    fence.dispatch(writeDeadline -> {
                        try {
                            return client.send(ProviderHttpRequest.newBuilder("POST",
                                    "http://127.0.0.1:" + server.getAddress().getPort() + "/items")
                                    .body("{}".getBytes(StandardCharsets.UTF_8)).build());
                        } catch (ProviderTransportException error) {
                            if (error.isProvablyNotSent()) {
                                provablyNotSent.incrementAndGet();
                            }
                            throw error;
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
    void readsChunkedAndFixedBodiesAndNeverFollowsRedirects() throws Exception {
        final HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/chunked", exchange -> {
            final byte[] received = readAll(exchange.getRequestBody());
            exchange.getResponseHeaders().add("X-Echo-Length", Integer.toString(received.length));
            exchange.getResponseHeaders().add("X-Echo-Method", exchange.getRequestMethod());
            exchange.sendResponseHeaders(201, 0);
            try (OutputStream output = exchange.getResponseBody()) {
                output.write("first-".getBytes(StandardCharsets.UTF_8));
                output.flush();
                output.write("second".getBytes(StandardCharsets.UTF_8));
            }
        });
        server.createContext("/redirect", exchange -> {
            exchange.getResponseHeaders().add("Location", "/chunked");
            exchange.sendResponseHeaders(302, -1);
            exchange.close();
        });
        server.start();
        try {
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.UNRESTRICTED);
            final String base = "http://127.0.0.1:" + server.getAddress().getPort();
            final ProviderHttpResponse chunked = client.send(ProviderHttpRequest.newBuilder("PATCH", base + "/chunked?a=1")
                    .header("Content-Type", "application/json").body("{\"x\":1}".getBytes(StandardCharsets.UTF_8))
                    .build());
            assertEquals(201, chunked.getStatusCode());
            assertEquals("7", chunked.getHeaders().first("x-echo-length"));
            assertEquals("PATCH", chunked.getHeaders().first("x-echo-method"));
            assertArrayEquals("first-second".getBytes(StandardCharsets.UTF_8), chunked.readBoundedBody(1024));
            assertEquals(302, client.send(ProviderHttpRequest.newBuilder("GET", base + "/redirect").build())
                    .getStatusCode());
            assertThrows(BodyTooLargeException.class, () -> client.send(ProviderHttpRequest.newBuilder("POST",
                    base + "/chunked").body(new byte[] {'{', '}'}).build()).readBoundedBody(5));
        } finally {
            server.stop(0);
        }
    }

    @Test
    void refusesInvalidMethodsAndHeadersBeforeAnyLookup() {
        final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(5), AddressPolicy.UNRESTRICTED);
        final String unresolvable = "https://provider.invalid/items";
        for (String method : new String[] {"GET\r\nX: y", "G T", "", "GET/1"}) {
            assertThrows(IllegalArgumentException.class,
                    () -> client.send(ProviderHttpRequest.newBuilder(method, unresolvable).build()), method);
        }
        for (String name : new String[] {"X-Bad:Name", "X Bad", "X-Bad\r\n", "é"}) {
            assertThrows(IllegalArgumentException.class, () -> client.send(
                    ProviderHttpRequest.newBuilder("GET", unresolvable).header(name, "value").build()), name);
        }
        for (String value : new String[] {"a\r\nb", "a\nb", "a\u0000b", "été", " padded"}) {
            assertThrows(IllegalArgumentException.class, () -> client.send(
                    ProviderHttpRequest.newBuilder("GET", unresolvable).header("X-Value", value).build()), value);
        }
        assertThrows(IllegalArgumentException.class, () -> client.send(
                ProviderHttpRequest.newBuilder("GET", unresolvable).body(new byte[] {1}).build()));
    }

    @Test
    void verifiesTlsTrustAndHostnameWithTheDefaultSocketFactory() throws Exception {
        final File directory = Files.createTempDirectory("sdkjava-tls").toFile();
        final File keystore = new File(directory, "server.p12");
        final Process keytool = new ProcessBuilder(System.getProperty("java.home") + "/bin/keytool", "-genkeypair",
                "-alias", "server", "-keyalg", "RSA", "-keysize", "2048", "-validity", "2",
                "-dname", "CN=localhost", "-ext", "SAN=dns:localhost", "-storetype", "PKCS12",
                "-keystore", keystore.getPath(), "-storepass", new String(PASSWORD), "-keypass", new String(PASSWORD))
                .redirectErrorStream(true).start();
        assertTrue(keytool.waitFor(60, TimeUnit.SECONDS));
        assertEquals(0, keytool.exitValue());
        final KeyStore store = KeyStore.getInstance("PKCS12");
        try (FileInputStream input = new FileInputStream(keystore)) {
            store.load(input, PASSWORD);
        }
        final KeyManagerFactory keys = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
        keys.init(store, PASSWORD);
        final SSLContext serverContext = SSLContext.getInstance("TLS");
        serverContext.init(keys.getKeyManagers(), null, null);
        final HttpsServer server = HttpsServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.setHttpsConfigurator(new HttpsConfigurator(serverContext));
        server.createContext("/ok", exchange -> {
            exchange.sendResponseHeaders(204, -1);
            exchange.close();
        });
        server.start();
        final SSLSocketFactory previous = HttpsURLConnection.getDefaultSSLSocketFactory();
        try {
            final int port = server.getAddress().getPort();
            final ProviderHttpClient untrusting = new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.UNRESTRICTED);
            final ProviderTransportException untrusted = assertThrows(ProviderTransportException.class,
                    () -> untrusting.send(ProviderHttpRequest.newBuilder("GET", "https://localhost:" + port + "/ok").build()));
            assertTrue(untrusted.isProvablyNotSent());

            final TrustManagerFactory trust = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
            trust.init(store);
            final SSLContext clientContext = SSLContext.getInstance("TLS");
            clientContext.init(null, trust.getTrustManagers(), null);
            HttpsURLConnection.setDefaultSSLSocketFactory(clientContext.getSocketFactory());
            final ProviderHttpClient trusting = new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.UNRESTRICTED);
            assertEquals(204, trusting.send(ProviderHttpRequest.newBuilder("GET", "https://localhost:" + port + "/ok")
                    .build()).getStatusCode());
            final ProviderTransportException mismatch = assertThrows(ProviderTransportException.class,
                    () -> trusting.send(ProviderHttpRequest.newBuilder("GET", "https://127.0.0.1:" + port + "/ok").build()));
            assertTrue(mismatch.isProvablyNotSent());
        } finally {
            HttpsURLConnection.setDefaultSSLSocketFactory(previous);
            server.stop(0);
        }
    }

    private static byte[] readAll(final InputStream input) throws IOException {
        final java.io.ByteArrayOutputStream output = new java.io.ByteArrayOutputStream();
        final byte[] buffer = new byte[1024];
        int read;
        while ((read = input.read(buffer)) >= 0) {
            output.write(buffer, 0, read);
        }
        return output.toByteArray();
    }
}
