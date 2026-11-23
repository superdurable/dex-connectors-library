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

    private static Call call(final FakeContext context) {
        return Call.create(CallId.of(CALL_ID), new ConnectionRef("sample", "main"),
                new OperationRef("sample", "createItem"), context.context(), 1);
    }
}
