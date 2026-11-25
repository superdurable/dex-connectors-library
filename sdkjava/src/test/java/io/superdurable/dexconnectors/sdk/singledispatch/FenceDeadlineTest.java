// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.sun.net.httpserver.HttpServer;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConformanceFixtures.FakeContext;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.ConnectorSteps;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.providerhttp.AddressPolicy;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpClient;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpRequest;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.time.Duration;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

/** Elapsed time is the behavior under test, so these tests wait on real clocks. */
class FenceDeadlineTest {
    private static final String CALL_ID = "5af8fafa-06ff-5b2d-bb13-04693809fabf";

    @Test
    void budgetsFromExecuteTimeLeftAndRefusesAnExpiredAttempt() throws Exception {
        final FakeContext context = new FakeContext("flow-42", "step-execution-7", 1, null);
        ConnectorSteps.attemptObservedAt(context.context());
        Thread.sleep(400);
        final Fence fence = new Fence(call(context), Duration.ofMillis(400), Fence.DEFAULT_WRITE_WINDOW_FRACTION, 1);
        assertEquals(DecisionKind.SEND_ALLOWED, fence.decide().getKind());
        assertThrows(InsufficientAttemptTimeException.class, () -> fence.dispatch(writeDeadline -> {
            throw new AssertionError("an expired attempt must not send");
        }));
        assertTrue(context.heartbeats().get(context.heartbeats().size() - 1).contains("\"state\":\"notSent\""));
    }

    @Test
    void timedOutSendForbidsCanResendAndClosesRegisteredIo() throws Exception {
        final FakeContext context = new FakeContext("flow-42", "step-execution-8", 1, null);
        final Fence fence = new Fence(call(context), Duration.ofMillis(600), 0.5, 1);
        fence.decide();
        final CountDownLatch closed = new CountDownLatch(1);
        assertThrows(WriteWindowTimeoutException.class, () -> fence.dispatch(writeDeadline -> {
            DispatchObservation.registerCancellation(closed::countDown);
            try {
                Thread.sleep(10_000);
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
            }
            return "late";
        }));
        assertTrue(closed.await(5, TimeUnit.SECONDS));
        assertThrows(DispatchNotAllowedException.class, fence::recordCanResend);
        assertThrows(DispatchNotAllowedException.class, () -> fence.recordProviderId("object-42"));
    }

    @Test
    void providerHttpReadsEndAtTheWriteDeadline() throws Exception {
        final HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        final CountDownLatch release = new CountDownLatch(1);
        server.createContext("/slow", exchange -> {
            try {
                release.await(10, TimeUnit.SECONDS);
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
            }
            exchange.sendResponseHeaders(200, -1);
            exchange.close();
        });
        server.start();
        try {
            final FakeContext context = new FakeContext("flow-42", "step-execution-9", 1, null);
            final Fence fence = new Fence(call(context), Duration.ofSeconds(1), 0.5, 1);
            fence.decide();
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(30), AddressPolicy.UNRESTRICTED);
            final long startedAt = System.nanoTime();
            final IOException error = assertThrows(IOException.class, () -> fence.dispatch(writeDeadline ->
                    client.send(ProviderHttpRequest.newBuilder("GET",
                            "http://127.0.0.1:" + server.getAddress().getPort() + "/slow").build())));
            assertTrue(System.nanoTime() - startedAt < TimeUnit.SECONDS.toNanos(5), error.toString());
            assertFalse(release.getCount() == 0);
        } finally {
            release.countDown();
            server.stop(0);
        }
    }

    @Test
    void cancelledAttemptNeverStartsTheSend() throws Exception {
        final FakeContext context = new FakeContext("flow-42", "step-execution-10", 1, null);
        final Fence fence = new Fence(call(context), Duration.ofSeconds(30), 0.5, 1);
        fence.decide();
        context.requestCancellation();
        assertThrows(InsufficientAttemptTimeException.class, () -> fence.dispatch(writeDeadline -> {
            throw new AssertionError("a cancelled attempt must not send");
        }));
    }

    @Test
    void cancellationBeforeTheRequestIsWrittenSendsNothing() throws Exception {
        final AtomicInteger requests = new AtomicInteger();
        final HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/items", exchange -> {
            requests.incrementAndGet();
            exchange.sendResponseHeaders(200, -1);
            exchange.close();
        });
        server.start();
        try {
            final FakeContext context = new FakeContext("flow-42", "step-execution-11", 1, null);
            // Its own Call ID, because this send outlives the Fence on purpose.
            final Fence fence = new Fence(callWithId(context, "3a6d73b9-2b75-503d-9247-2a8dc2fa961e"),
                    Duration.ofSeconds(30), 0.5, 1);
            fence.decide();
            final CountDownLatch entered = new CountDownLatch(1);
            final CountDownLatch release = new CountDownLatch(1);
            final CountDownLatch sendReturned = new CountDownLatch(1);
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(30), AddressPolicy.UNRESTRICTED);
            final Thread handler = new Thread(() -> {
                try {
                    fence.dispatch(writeDeadline -> {
                        entered.countDown();
                        awaitIgnoringInterrupts(release);
                        try {
                            return client.send(ProviderHttpRequest.newBuilder("POST",
                                    "http://127.0.0.1:" + server.getAddress().getPort() + "/items")
                                    .body(new byte[] {'{', '}'}).build());
                        } finally {
                            sendReturned.countDown();
                        }
                    });
                } catch (InterruptedException | IOException expected) {
                    // Cancellation ends the dispatch; the assertions below check the provider.
                }
            });
            handler.start();
            assertTrue(entered.await(5, TimeUnit.SECONDS));
            context.requestCancellation();
            handler.join(5_000);
            assertFalse(handler.isAlive());
            release.countDown();
            assertTrue(sendReturned.await(5, TimeUnit.SECONDS));
            assertEquals(0, requests.get());
        } finally {
            server.stop(0);
        }
    }

    @Test
    void cancellationDuringAnInFlightRequestClosesIt() throws Exception {
        final CountDownLatch received = new CountDownLatch(1);
        final CountDownLatch release = new CountDownLatch(1);
        final HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/items", exchange -> {
            received.countDown();
            try {
                release.await(10, TimeUnit.SECONDS);
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
            }
            exchange.sendResponseHeaders(200, -1);
            exchange.close();
        });
        server.start();
        try {
            final FakeContext context = new FakeContext("flow-42", "step-execution-12", 1, null);
            final Fence fence = new Fence(call(context), Duration.ofSeconds(30), 0.5, 1);
            fence.decide();
            final ProviderHttpClient client = new ProviderHttpClient(Duration.ofSeconds(30), AddressPolicy.UNRESTRICTED);
            final Thread canceller = new Thread(() -> {
                try {
                    if (received.await(5, TimeUnit.SECONDS)) {
                        context.requestCancellation();
                    }
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                }
            });
            canceller.start();
            final long startedAt = System.nanoTime();
            assertThrows(InterruptedException.class, () -> fence.dispatch(writeDeadline -> client.send(
                    ProviderHttpRequest.newBuilder("POST", "http://127.0.0.1:" + server.getAddress().getPort() + "/items")
                            .body(new byte[] {'{', '}'}).build())));
            assertTrue(System.nanoTime() - startedAt < TimeUnit.SECONDS.toNanos(5));
            assertThrows(DispatchNotAllowedException.class, fence::recordCanResend);
            canceller.join();
        } finally {
            release.countDown();
            server.stop(0);
        }
    }

    @Test
    void anUnstoppedSendBlocksAnotherSendOfTheSameCall() throws Exception {
        final String callId = "0b9d3c3e-6a51-5f0e-9a43-7d2f1f7c2b10";
        final FakeContext context = new FakeContext("flow-42", "step-execution-13", 1, null);
        final Fence fence = new Fence(callWithId(context, callId), Duration.ofMillis(600), 0.5, 3);
        fence.decide();
        final CountDownLatch release = new CountDownLatch(1);
        assertThrows(WriteWindowTimeoutException.class, () -> fence.dispatch(writeDeadline -> {
            awaitIgnoringInterrupts(release);
            return "late";
        }));
        final FakeContext retry = new FakeContext("flow-42", "step-execution-13", 2, null);
        assertEquals(DecisionKind.READ_BACK_REQUIRED,
                new Fence(callWithId(retry, callId), Duration.ofSeconds(30), 0.5, 3).decide().getKind());
        release.countDown();
    }

    @Test
    void aLiveCallbackBlocksAnotherAttemptEvenWithACachedDecision() throws Exception {
        final String callId = "e18c5cc6-9bd8-56c4-8557-f0cd415e65d4";
        final FakeContext first = new FakeContext("flow-42", "step-execution-14", 1, null);
        final Fence firstFence = new Fence(callWithId(first, callId), Duration.ofMillis(600), 0.5, 3);
        firstFence.decide();
        final FakeContext cachedRetry = new FakeContext("flow-42", "step-execution-14", 2, null);
        final Fence cachedFence = new Fence(callWithId(cachedRetry, callId), Duration.ofSeconds(30), 0.5, 3);
        cachedFence.decide();
        final CountDownLatch release = new CountDownLatch(1);
        final AtomicInteger callbacks = new AtomicInteger();
        assertThrows(WriteWindowTimeoutException.class, () -> firstFence.dispatch(writeDeadline -> {
            callbacks.incrementAndGet();
            awaitIgnoringInterrupts(release);
            return "late";
        }));
        final String claimed = first.heartbeats().get(first.heartbeats().size() - 1);
        final FakeContext retry = new FakeContext("flow-42", "step-execution-14", 2,
                io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER.readTree(claimed));
        assertEquals(DecisionKind.READ_BACK_REQUIRED,
                new Fence(callWithId(retry, callId), Duration.ofSeconds(30), 0.5, 3).decide().getKind());
        assertThrows(DispatchNotAllowedException.class, () -> cachedFence.dispatch(writeDeadline -> {
            callbacks.incrementAndGet();
            return "overlap";
        }));
        assertEquals(1, callbacks.get());
        release.countDown();
        final long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5);
        DecisionKind afterRelease = DecisionKind.READ_BACK_REQUIRED;
        while (System.nanoTime() < deadline && afterRelease == DecisionKind.READ_BACK_REQUIRED) {
            afterRelease = new Fence(callWithId(new FakeContext("flow-42", "step-execution-14", 2,
                    io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER.readTree(claimed)), callId),
                    Duration.ofSeconds(30), 0.5, 3).decide().getKind();
            Thread.sleep(10);
        }
        assertEquals(DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, afterRelease);
    }

    @Test
    void aStoppedCallbackEndsTheStopWaitWithoutTheFullGrace() throws Exception {
        final FakeContext context = new FakeContext("flow-42", "step-execution-15", 1, null);
        final Fence fence = new Fence(callWithId(context, "391bf0f6-f405-5d7a-9b2c-e5eb439e1910"),
                Duration.ofMillis(400), 0.5, 1);
        fence.decide();
        final long startedAt = System.nanoTime();
        assertThrows(WriteWindowTimeoutException.class, () -> fence.dispatch(writeDeadline -> {
            Thread.sleep(10_000);
            return "late";
        }));
        assertTrue(System.nanoTime() - startedAt < TimeUnit.MILLISECONDS.toNanos(900));
        assertEquals(DecisionKind.SEND_ALLOWED, new Fence(callWithId(
                new FakeContext("flow-42", "step-execution-16", 1, null), "391bf0f6-f405-5d7a-9b2c-e5eb439e1910"),
                Duration.ofSeconds(30), 0.5, 1).decide().getKind());
    }

    private static void awaitIgnoringInterrupts(final CountDownLatch latch) {
        boolean isInterrupted = false;
        while (true) {
            try {
                if (latch.await(10, TimeUnit.SECONDS)) {
                    break;
                }
            } catch (InterruptedException ignored) {
                // Simulates provider code that does not honor interruption.
                isInterrupted = true;
            }
        }
        if (isInterrupted) {
            Thread.currentThread().interrupt();
        }
    }

    private static Call call(final FakeContext context) {
        return callWithId(context, CALL_ID);
    }

    private static Call callWithId(final FakeContext context, final String callId) {
        return Call.create(CallId.of(callId), new ConnectionRef("sample", "main"),
                new OperationRef("sample", "createItem"), context.context(), 1);
    }
}
