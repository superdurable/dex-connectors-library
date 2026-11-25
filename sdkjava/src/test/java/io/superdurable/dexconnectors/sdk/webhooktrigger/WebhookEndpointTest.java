// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.CredentialProvider;
import io.superdurable.dexconnectors.sdk.projectconfig.CredentialBrokerUnavailableException;
import io.superdurable.dexconnectors.sdk.trigger.TriggerBindingRef;
import io.superdurable.dexconnectors.sdk.trigger.TriggerCancellation;
import io.superdurable.dexconnectors.sdk.trigger.TriggerConfig;
import io.superdurable.dexconnectors.sdk.trigger.TriggerContext;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDefinition;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDeliveryAbortedException;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDeliveryPreparer;
import io.superdurable.dexconnectors.sdk.trigger.TriggerEvent;
import io.superdurable.dexconnectors.sdk.trigger.TriggerRef;
import io.superdurable.dexconnectors.sdk.trigger.TriggerRunner;
import io.superdurable.dexconnectors.sdk.trigger.TriggerTarget;
import io.superdurable.dexconnectors.sdk.trigger.Triggers;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.InetSocketAddress;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.AbstractMap.SimpleImmutableEntry;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;

class WebhookEndpointTest {
    private static final ConnectionRef CONNECTION = new ConnectionRef("sample", "main");
    private static final Instant RECEIVED_AT = Instant.parse("2025-10-12T00:00:00Z");
    private static final List<Map.Entry<String, String>> HEADERS =
            Collections.<Map.Entry<String, String>>singletonList(new SimpleImmutableEntry<>("X-Signature", "ok"));

    @Test
    void answers503WithoutASourceThenRecordsBefore200InOrder() throws Exception {
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(config(credentials()).build());
        assertEquals(503, post(endpoint, "Ev1").getStatus());
        final List<String> delivered = new CopyOnWriteArrayList<>();
        final RunningSource running = run(endpoint, endpoint.newSource(null), (context, event) -> delivered.add(event.getId()));
        try {
            awaitCondition(() -> endpoint.runningSourceCount() == 1);
            final WebhookResponse response = post(endpoint, "Ev1");
            assertEquals(200, response.getStatus());
            assertEquals(200, post(endpoint, "Ev2").getStatus());
            awaitCondition(() -> delivered.size() == 2);
            assertEquals(Arrays.asList("Ev1", "Ev2"), delivered);
            final WebhookResponse empty = post(endpoint, "");
            assertEquals(200, empty.getStatus());
        } finally {
            running.stop();
        }
    }

    @Test
    void verificationFailuresMapTo400Or503AndCredentialsUseAStableCall() throws Exception {
        final List<Call> calls = new CopyOnWriteArrayList<>();
        final CredentialProvider<String> recording = call -> {
            calls.add(call);
            return "secret";
        };
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(WebhookEndpointConfig
                .<String, String>newBuilder("sample", "itemCreated", CONNECTION, recording,
                        (request, credentials, cancellation) -> {
                            final String signature = request.header("x-signature");
                            if ("unavailable".equals(signature)) {
                                throw new WebhookVerificationUnavailableException(null);
                            }
                            if (!"ok".equals(signature) || !"secret".equals(credentials)) {
                                throw new IllegalArgumentException("refused");
                            }
                        }, WebhookEndpointTest::decode, 1024).build());
        assertEquals(400, endpoint.handleRequest("POST", "/hook", pairs("x-signature", "bad"), body("Ev1"), null)
                .getStatus());
        assertEquals(503, endpoint.handleRequest("POST", "/hook", pairs("x-signature", "unavailable"), body("Ev1"),
                null).getStatus());
        assertEquals(400, endpoint.handleRequest("POST", "/hook", HEADERS, "{".getBytes(StandardCharsets.UTF_8), null)
                .getStatus());
        assertEquals(405, endpoint.handleRequest("GET", "/hook", HEADERS, body("Ev1"), null).getStatus());
        final WebhookResponse tooLarge = endpoint.handleRequest("POST", "/hook", HEADERS, new byte[1025], null);
        assertEquals(413, tooLarge.getStatus());
        assertEquals("request body too large\n", tooLarge.getBodyText());
        assertEquals(calls.get(0).getId(), calls.get(1).getId());
        assertEquals("itemCreated", calls.get(0).getOperation().getOperationId());
        assertEquals(WebhookEndpoint.credentialCallId("sample", CONNECTION, body("Ev1")), calls.get(0).getId());

        final WebhookEndpoint<String, String> failing = new WebhookEndpoint<>(WebhookEndpointConfig
                .<String, String>newBuilder("sample", "itemCreated", CONNECTION, call -> {
                    throw new IOException("store unavailable");
                }, (request, credentials, cancellation) -> { }, WebhookEndpointTest::decode, 1024).build());
        assertEquals(503, failing.handleRequest("POST", "/hook", HEADERS, body("Ev1"), null).getStatus());
    }

    @Test
    void rejectsInvalidEndpointConfiguration() {
        assertThrows(IllegalArgumentException.class, () -> new WebhookEndpoint<>(WebhookEndpointConfig
                .<String, String>newBuilder(" ", "itemCreated", CONNECTION, credentials(), (r, c, x) -> { },
                        WebhookEndpointTest::decode, 1024).build()));
        assertThrows(IllegalArgumentException.class, () -> new WebhookEndpoint<>(config(credentials())
                .arrivalTimeout(Duration.ZERO).build()));
        assertThrows(IllegalArgumentException.class, () -> new WebhookEndpoint<>(config(credentials())
                .sourceQueueCapacity(0).build()));
        assertThrows(IllegalArgumentException.class, () -> new WebhookEndpoint<>(WebhookEndpointConfig
                .<String, String>newBuilder("sample", "itemCreated", CONNECTION, credentials(), (r, c, x) -> { },
                        WebhookEndpointTest::decode, 0).build()));
    }

    @Test
    void keepsArrivalOrderWhenAnEarlierRecordIsSlower() throws Exception {
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(config(credentials()).build());
        final CountDownLatch releaseFirst = new CountDownLatch(1);
        final CountDownLatch firstPreparing = new CountDownLatch(1);
        final List<String> prepared = new CopyOnWriteArrayList<>();
        final List<String> delivered = new CopyOnWriteArrayList<>();
        final RunningSource running = run(endpoint, endpoint.newSource(null), new PreparingTarget(prepared, delivered,
                event -> {
                    if ("Ev1".equals(event.getId())) {
                        firstPreparing.countDown();
                        releaseFirst.await();
                    }
                }));
        final ExecutorService requests = Executors.newFixedThreadPool(2);
        try {
            awaitCondition(() -> endpoint.runningSourceCount() == 1);
            final Future<WebhookResponse> first = requests.submit(() -> post(endpoint, "Ev1"));
            assertTrue(firstPreparing.await(10, TimeUnit.SECONDS));
            final Future<WebhookResponse> second = requests.submit(() -> post(endpoint, "Ev2"));
            releaseFirst.countDown();
            assertEquals(200, first.get(10, TimeUnit.SECONDS).getStatus());
            assertEquals(200, second.get(10, TimeUnit.SECONDS).getStatus());
            awaitCondition(() -> delivered.size() == 2);
            assertEquals(Arrays.asList("Ev1", "Ev2"), prepared);
            assertEquals(Arrays.asList("Ev1", "Ev2"), delivered);
        } finally {
            requests.shutdownNow();
            running.stop();
        }
    }

    @Test
    void slotsExpireFromReservationAndLateWritesStillQueue() throws Exception {
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(config(credentials())
                .arrivalTimeout(Duration.ofMillis(200)).build());
        final CountDownLatch releaseWrite = new CountDownLatch(1);
        final List<String> prepared = new CopyOnWriteArrayList<>();
        final List<String> delivered = new CopyOnWriteArrayList<>();
        final RunningSource running = run(endpoint, endpoint.newSource(null), new PreparingTarget(prepared, delivered,
                event -> releaseWrite.await()));
        try {
            awaitCondition(() -> endpoint.runningSourceCount() == 1);
            assertEquals(503, post(endpoint, "Ev1").getStatus());
            releaseWrite.countDown();
            awaitCondition(() -> delivered.size() == 1);
            assertEquals(Collections.singletonList("Ev1"), delivered);
        } finally {
            running.stop();
        }
    }

    @Test
    void recordingFailuresAndFullQueuesAnswer503() throws Exception {
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(config(credentials())
                .sourceQueueCapacity(1).build());
        final CountDownLatch releaseDelivery = new CountDownLatch(1);
        final CountDownLatch deliveryStarted = new CountDownLatch(1);
        final List<String> prepared = new CopyOnWriteArrayList<>();
        final List<String> delivered = new CopyOnWriteArrayList<>();
        final RunningSource running = run(endpoint, endpoint.newSource(null), new PreparingTarget(prepared, delivered,
                event -> {
                    if ("fail".equals(event.getPayload())) {
                        throw new IOException("inbox unavailable");
                    }
                }) {
            @Override
            public void handleTrigger(final TriggerContext context, final TriggerEvent<String> event) throws Exception {
                deliveryStarted.countDown();
                releaseDelivery.await();
                super.handleTrigger(context, event);
            }
        });
        try {
            awaitCondition(() -> endpoint.runningSourceCount() == 1);
            assertEquals(503, endpoint.handleRequest("POST", "/hook", HEADERS,
                    "{\"id\":\"Ev0\",\"payload\":\"fail\"}".getBytes(StandardCharsets.UTF_8), null).getStatus());
            assertEquals(200, post(endpoint, "Ev1").getStatus());
            assertTrue(deliveryStarted.await(10, TimeUnit.SECONDS));
            assertEquals(200, post(endpoint, "Ev2").getStatus());
            final WebhookResponse full = post(endpoint, "Ev3");
            assertEquals(503, full.getStatus());
            assertEquals("webhook delivery temporarily unavailable\n", full.getBodyText());
            releaseDelivery.countDown();
            awaitCondition(() -> delivered.size() == 2);
        } finally {
            releaseDelivery.countDown();
            running.stop();
        }
    }

    @Test
    void answers503UntilEveryCreatedSourceRuns() throws Exception {
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(config(credentials()).build());
        final WebhookTriggerSource<String> first = endpoint.newSource(null);
        endpoint.newSource(event -> false);
        final RunningSource running = run(endpoint, first, (context, event) -> { });
        try {
            awaitCondition(() -> endpoint.runningSourceCount() == 1);
            assertEquals(503, post(endpoint, "Ev1").getStatus());
        } finally {
            running.stop();
        }
    }

    @Test
    void servesTheRawRequestTargetThroughTheJdkHttpServer() throws Exception {
        final List<String> urls = new CopyOnWriteArrayList<>();
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(WebhookEndpointConfig
                .<String, String>newBuilder("sample", "itemCreated", CONNECTION, credentials(),
                        (request, credentials, cancellation) -> {
                            urls.add(request.getUrl());
                            if (!"ok".equals(request.header("X-SIGNATURE"))) {
                                throw new IllegalArgumentException("refused");
                            }
                        }, WebhookEndpointTest::decode, 64).build());
        final List<String> delivered = new CopyOnWriteArrayList<>();
        final RunningSource running = run(endpoint, endpoint.newSource(null), (context, event) -> delivered.add(event.getId()));
        final HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/hooks", new WebhookHttpHandler(endpoint));
        server.setExecutor(Executors.newCachedThreadPool());
        server.start();
        try {
            awaitCondition(() -> endpoint.runningSourceCount() == 1);
            final String base = "http://127.0.0.1:" + server.getAddress().getPort();
            assertEquals(200, httpPost(base + "/hooks/a%2Fb?x=1&y=%20", "ok", body("Ev1")));
            assertEquals("/hooks/a%2Fb?x=1&y=%20", urls.get(0));
            assertEquals(400, httpPost(base + "/hooks", "bad", body("Ev2")));
            assertEquals(413, httpPost(base + "/hooks", "ok", new byte[65]));
            awaitCondition(() -> delivered.size() == 1);
        } finally {
            server.stop(0);
            running.stop();
        }
    }

    @Test
    void relayedVerificationMapsBrokerFailures() throws Exception {
        final WebhookRequestVerifier<String> unavailable = RelayedRequestVerification.newRelayedRequestVerification(
                (headers, body, receivedAt) -> {
                    throw new CredentialBrokerUnavailableException("broker down", null);
                });
        final WebhookRequest request = new WebhookRequest("/hook", HEADERS, body("Ev1"), RECEIVED_AT);
        assertThrows(WebhookVerificationUnavailableException.class,
                () -> unavailable.verifyRequest(request, "", new TriggerCancellation()));
        final WebhookRequestVerifier<String> refused = RelayedRequestVerification.newRelayedRequestVerification(
                (headers, body, receivedAt) -> {
                    throw new IllegalArgumentException("mismatch");
                });
        assertThrows(IllegalArgumentException.class, () -> refused.verifyRequest(request, "", new TriggerCancellation()));
        final List<Instant> receipts = new ArrayList<>();
        RelayedRequestVerification.<String>newRelayedRequestVerification((headers, body, receivedAt) -> {
            receipts.add(receivedAt);
            assertEquals("x-signature", headers.get(0).getKey());
        }).verifyRequest(request, "", new TriggerCancellation());
        assertEquals(Collections.singletonList(RECEIVED_AT), receipts);
        assertThrows(IllegalArgumentException.class, () -> RelayedRequestVerification.newRelayedRequestVerification(null));
    }

    @Test
    void endpointRunnerStopsTheOthersWhenOneFailsAndReportsAborts() throws Exception {
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(config(credentials()).build());
        final TriggerDefinition definition = new TriggerDefinition(new TriggerRef("sample", "itemCreated"), "Item");
        final TriggerRunner webhookRunner = Triggers.newTrigger(new TriggerConfig<String>(definition,
                new TriggerBindingRef(CONNECTION, definition.getTrigger(), "orders"), endpoint.newSource(null),
                (context, event) -> { }, null));
        final TriggerRunner failingRunner = Triggers.newTrigger(new TriggerConfig<String>(definition,
                new TriggerBindingRef(CONNECTION, definition.getTrigger(), "audit"), (context, target) -> {
                    throw new IllegalStateException("source failed");
                }, (context, event) -> { }, null));
        final WebhookEndpointRunner runner = new WebhookEndpointRunner(endpoint, Arrays.asList(webhookRunner, failingRunner));
        final IllegalStateException failure = assertThrows(IllegalStateException.class, () -> runner.run(null));
        assertEquals("source failed", failure.getMessage());
        assertEquals(0, endpoint.runningSourceCount());

        final TriggerCancellation cancellation = new TriggerCancellation();
        final WebhookEndpointRunner aborting = new WebhookEndpointRunner(endpoint, Collections.singletonList(webhookRunner));
        final ExecutorService executor = Executors.newSingleThreadExecutor();
        try {
            final Future<?> run = executor.submit(() -> {
                aborting.run(new TriggerContext(cancellation));
                return null;
            });
            awaitCondition(() -> endpoint.runningSourceCount() == 1);
            cancellation.cancel();
            final Exception aborted = assertThrows(Exception.class, () -> run.get(10, TimeUnit.SECONDS));
            assertTrue(aborted.getCause() instanceof TriggerDeliveryAbortedException);
        } finally {
            executor.shutdownNow();
        }
    }

    @Test
    void startedRunnerRestartsAfterAFailureAndReportsIt() throws Exception {
        final WebhookEndpoint<String, String> endpoint = new WebhookEndpoint<>(config(credentials()).build());
        final TriggerDefinition definition = new TriggerDefinition(new TriggerRef("sample", "itemCreated"), "Item");
        final CountDownLatch secondRun = new CountDownLatch(2);
        final TriggerRunner failingRunner = Triggers.newTrigger(new TriggerConfig<String>(definition,
                new TriggerBindingRef(CONNECTION, definition.getTrigger(), "orders"), (context, target) -> {
                    secondRun.countDown();
                    if (secondRun.getCount() > 0) {
                        throw new IllegalStateException("transient");
                    }
                    context.getCancellation().awaitCancellation();
                    context.getCancellation().throwIfCancelled();
                }, (context, event) -> { }, null));
        final WebhookEndpointRunner runner = new WebhookEndpointRunner(endpoint, Collections.singletonList(failingRunner));
        runner.start();
        try {
            assertTrue(secondRun.await(10, TimeUnit.SECONDS));
            awaitCondition(() -> runner.getRunFailure() == null);
        } finally {
            runner.close();
        }
    }

    private static WebhookEndpointConfig.Builder<String, String> config(final CredentialProvider<String> credentials) {
        return WebhookEndpointConfig.<String, String>newBuilder("sample", "itemCreated", CONNECTION, credentials,
                (request, secret, cancellation) -> {
                    if (!"ok".equals(request.header("x-signature"))) {
                        throw new IllegalArgumentException("refused");
                    }
                }, WebhookEndpointTest::decode, 1024).clock(Clock.fixed(RECEIVED_AT, ZoneOffset.UTC));
    }

    private static CredentialProvider<String> credentials() {
        return call -> "secret";
    }

    private static TriggerEvent<String> decode(final WebhookRequest request) {
        final String text = new String(request.getBody(), StandardCharsets.UTF_8);
        if (!text.startsWith("{\"id\":\"")) {
            throw new IllegalArgumentException("undecodable");
        }
        final String id = text.substring(7, text.indexOf('"', 7));
        if (id.isEmpty()) {
            return null;
        }
        final int payloadStart = text.indexOf("\"payload\":\"");
        final String payload = payloadStart < 0 ? "" : text.substring(payloadStart + 11, text.indexOf('"', payloadStart + 11));
        return new TriggerEvent<>(id, OffsetDateTime.of(2025, 10, 12, 0, 0, 0, 0, ZoneOffset.UTC), payload);
    }

    private static byte[] body(final String id) {
        return ("{\"id\":\"" + id + "\"}").getBytes(StandardCharsets.UTF_8);
    }

    private static WebhookResponse post(final WebhookEndpoint<String, String> endpoint, final String id)
            throws InterruptedException {
        return endpoint.handleRequest("POST", "/hook", HEADERS, body(id), null);
    }

    private static List<Map.Entry<String, String>> pairs(final String name, final String value) {
        return Collections.<Map.Entry<String, String>>singletonList(new SimpleImmutableEntry<>(name, value));
    }

    private static int httpPost(final String url, final String signature, final byte[] body) throws IOException {
        final HttpURLConnection connection = (HttpURLConnection) new URL(url).openConnection();
        connection.setRequestMethod("POST");
        connection.setDoOutput(true);
        connection.setRequestProperty("X-Signature", signature);
        try (OutputStream output = connection.getOutputStream()) {
            output.write(body);
        }
        final int status = connection.getResponseCode();
        final InputStream input = status >= 400 ? connection.getErrorStream() : connection.getInputStream();
        if (input != null) {
            final ByteArrayOutputStream ignored = new ByteArrayOutputStream();
            final byte[] buffer = new byte[256];
            int read;
            while ((read = input.read(buffer)) >= 0) {
                ignored.write(buffer, 0, read);
            }
            input.close();
        }
        connection.disconnect();
        return status;
    }

    private static void awaitCondition(final BooleanSupplier condition) throws InterruptedException {
        final long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
        while (!condition.getAsBoolean()) {
            if (System.nanoTime() > deadline) {
                throw new AssertionError("condition did not converge within 10 seconds");
            }
            Thread.sleep(10);
        }
    }

    private static RunningSource run(
            final WebhookEndpoint<String, String> endpoint, final WebhookTriggerSource<String> source,
            final TriggerTarget<String> target) {
        return new RunningSource(source, target);
    }

    private interface EventAction {
        void run(TriggerEvent<String> event) throws Exception;
    }

    private static class PreparingTarget implements TriggerTarget<String>, TriggerDeliveryPreparer<String> {
        private final List<String> prepared;
        private final List<String> delivered;
        private final EventAction onPrepare;

        PreparingTarget(final List<String> prepared, final List<String> delivered, final EventAction onPrepare) {
            this.prepared = prepared;
            this.delivered = delivered;
            this.onPrepare = onPrepare;
        }

        @Override
        public void prepareTrigger(final TriggerContext context, final TriggerEvent<String> event) throws Exception {
            onPrepare.run(event);
            prepared.add(event.getId());
        }

        @Override
        public void handleTrigger(final TriggerContext context, final TriggerEvent<String> event) throws Exception {
            delivered.add(event.getId());
        }
    }

    private static final class RunningSource {
        private final TriggerCancellation cancellation = new TriggerCancellation();
        private final Thread thread;

        RunningSource(final WebhookTriggerSource<String> source, final TriggerTarget<String> target) {
            thread = new Thread(() -> {
                try {
                    source.run(new TriggerContext(cancellation), target);
                } catch (TriggerDeliveryAbortedException | InterruptedException expected) {
                    // The test stopped the source.
                }
            });
            thread.start();
        }

        void stop() throws InterruptedException {
            cancellation.cancel();
            thread.join(TimeUnit.SECONDS.toMillis(10));
        }
    }
}
