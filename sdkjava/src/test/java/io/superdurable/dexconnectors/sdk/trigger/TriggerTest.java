// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertSame;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import io.grpc.Status;
import io.superdurable.dex.exceptions.DexServiceException;
import io.superdurable.dex.exceptions.ErrorSubStatus;
import io.superdurable.dex.exceptions.FlowNotActiveOrNotFoundException;
import io.superdurable.dex.exceptions.FlowNotFoundException;
import io.superdurable.dex.exceptions.ValueMappingException;
import io.superdurable.dex.exceptions.WorkerInvocationException;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.ConnectorConfigurationUI;
import java.time.Duration;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.logging.Handler;
import java.util.logging.LogRecord;
import java.util.logging.Logger;
import org.junit.jupiter.api.Test;

class TriggerTest {
    private static final TriggerEvent<String> EVENT =
            new TriggerEvent<>("Ev1", OffsetDateTime.of(2025, 10, 12, 0, 0, 0, 0, ZoneOffset.UTC), "hello");
    private static final TriggerRef TRIGGER = new TriggerRef("sample", "itemCreated");
    private static final TriggerDefinition DEFINITION = new TriggerDefinition(TRIGGER, "Item created");
    private static final TriggerBindingRef BINDING =
            new TriggerBindingRef(new ConnectionRef("sample", "main"), TRIGGER, "orders");

    @Test
    void retryDelayScheduleDoublesToTheCap() {
        final List<Long> delays = new ArrayList<>();
        for (int attempt = 1; attempt <= 10; attempt++) {
            delays.add(TriggerDelivery.triggerRetryDelay(attempt).toMillis());
        }
        assertEquals(Arrays.asList(250L, 500L, 1000L, 2000L, 4000L, 8000L, 16000L, 30000L, 30000L, 30000L), delays);
    }

    @Test
    void retriesAndLogsRecoveryWithoutMessages() throws Exception {
        final RecordingTriggerLogger logger = new RecordingTriggerLogger();
        final AtomicInteger attempts = new AtomicInteger();
        TriggerDelivery.deliverTrigger(new TriggerContext(), (context, event) -> {
            if (attempts.incrementAndGet() == 1) {
                throw new IllegalStateException("secret payload text");
            }
        }, EVENT, logger);
        assertEquals(2, attempts.get());
        final List<RecordingTriggerLogger.Record> records = logger.records();
        assertEquals("trigger delivery failed; retrying", records.get(0).message);
        assertEquals(TriggerLogLevel.WARN, records.get(0).level);
        assertEquals(250L, records.get(0).attributes.get("delay_ms"));
        assertEquals("IllegalStateException", records.get(0).attributes.get("error_type"));
        assertFalse(records.get(0).attributes.toString().contains("secret"));
        assertEquals("trigger delivered after retry", records.get(1).message);
        assertEquals(2, records.get(1).attributes.get("attempts"));
    }

    @Test
    void consumesUndeliverableAndAbortsWaits() throws Exception {
        final RecordingTriggerLogger logger = new RecordingTriggerLogger();
        TriggerDelivery.deliverTrigger(new TriggerContext(), (context, event) -> {
            throw new UndeliverableTriggerException("bad");
        }, EVENT, logger);
        assertEquals("trigger event skipped: undeliverable", logger.records().get(0).message);

        final TriggerCancellation cancellation = new TriggerCancellation();
        cancellation.cancel();
        assertThrows(TriggerDeliveryAbortedException.class,
                () -> TriggerDelivery.deliverTrigger(new TriggerContext(cancellation), (context, event) -> { }, EVENT,
                        logger));
    }

    @Test
    void cancellationDuringARealWaitAborts() throws Exception {
        final TriggerCancellation cancellation = new TriggerCancellation();
        final RecordingTriggerLogger logger = new RecordingTriggerLogger();
        final Thread canceller = new Thread(() -> {
            while (logger.records().isEmpty()) {
                Thread.yield();
            }
            cancellation.cancel();
        });
        canceller.start();
        final long startedAt = System.nanoTime();
        assertThrows(TriggerDeliveryAbortedException.class, () -> TriggerDelivery.deliverTrigger(
                new TriggerContext(cancellation), (context, event) -> {
                    throw new IllegalStateException("retry");
                }, EVENT, logger));
        canceller.join();
        assertTrue(Duration.ofNanos(System.nanoTime() - startedAt).compareTo(Duration.ofSeconds(5)) < 0);
        // The failed attempt after cancellation reports no retry.
        assertEquals(1, logger.records().size());
    }

    @Test
    void interruptionPropagatesUnchanged() {
        assertThrows(InterruptedException.class, () -> TriggerDelivery.deliverTrigger(new TriggerContext(),
                (context, event) -> {
                    throw new InterruptedException();
                }, EVENT, new RecordingTriggerLogger()));
    }

    @Test
    void marksOnceAndFindsWrappedMarkers() {
        final UndeliverableTriggerException marker = new UndeliverableTriggerException("bad");
        assertSame(marker, TriggerDelivery.markTriggerUndeliverable(marker));
        final IllegalStateException failure = new IllegalStateException("boom");
        final UndeliverableTriggerException wrapped = TriggerDelivery.markTriggerUndeliverable(failure);
        assertSame(failure, wrapped.getCause());
        assertEquals("Trigger event is undeliverable: boom", wrapped.getMessage());
        assertEquals("Trigger event is undeliverable", new UndeliverableTriggerException().getMessage());
        assertTrue(TriggerDelivery.isTriggerUndeliverable(new RuntimeException(new RuntimeException(marker))));
        assertFalse(TriggerDelivery.isTriggerUndeliverable(null));
        assertFalse(TriggerDelivery.isTriggerUndeliverable(failure));
    }

    @Test
    void causeSearchStopsAfter32Links() {
        Throwable chain = new UndeliverableTriggerException();
        for (int link = 0; link < 31; link++) {
            chain = new RuntimeException(chain);
        }
        assertTrue(TriggerDelivery.isTriggerUndeliverable(chain));
        assertFalse(TriggerDelivery.isTriggerUndeliverable(new RuntimeException(chain)));
    }

    @Test
    void errorAttributesIncludeGrpcCodes() {
        final Map<String, Object> attributes = TriggerDelivery.triggerErrorAttributes(
                new DexServiceException(Status.Code.UNAVAILABLE, ErrorSubStatus.UNCATEGORIZED, "secret",
                        new IllegalStateException("x")));
        assertEquals("DexServiceException", attributes.get("error_type"));
        assertEquals("IllegalStateException", attributes.get("error_cause_type"));
        assertEquals(14, attributes.get("error_code"));
        assertEquals("Error", TriggerDelivery.triggerErrorAttributes(new RuntimeException() { }).get("error_type"));
    }

    @Test
    void skipInsideAnAttemptSuppressesRecoveryRecord() throws Exception {
        final RecordingTriggerLogger logger = new RecordingTriggerLogger();
        final AtomicInteger attempts = new AtomicInteger();
        TriggerDelivery.deliverTrigger(new TriggerContext(), (context, event) -> {
            if (attempts.incrementAndGet() == 1) {
                throw new IllegalStateException("retry");
            }
            context.recordSkip();
        }, EVENT, logger);
        assertEquals(1, logger.records().size());
    }

    @Test
    void standardLoggerWritesSortedAttributes() {
        final Logger julLogger = Logger.getLogger("dex.connectors.test.trigger");
        final List<String> messages = new ArrayList<>();
        julLogger.setUseParentHandlers(false);
        julLogger.addHandler(new Handler() {
            @Override
            public void publish(final LogRecord record) {
                messages.add(record.getLevel() + " " + record.getMessage());
            }

            @Override
            public void flush() {
            }

            @Override
            public void close() {
            }
        });
        final StandardTriggerLogger logger = new StandardTriggerLogger(julLogger);
        final Map<String, Object> attributes = new java.util.LinkedHashMap<>();
        attributes.put("z", 1);
        attributes.put("a", "é");
        logger.log(TriggerLogLevel.WARN, "trigger delivery failed; retrying", attributes);
        logger.log(TriggerLogLevel.INFO, "plain", Collections.<String, Object>emptyMap());
        assertEquals(Arrays.asList("WARNING trigger delivery failed; retrying {\"a\":\"é\",\"z\":1}", "INFO plain"),
                messages);
        assertFalse(logger.isEnabledFor(TriggerLogLevel.DEBUG));
    }

    @Test
    void runnerReplaysPreparesAndConsumesWithBindingIdentity() throws Exception {
        final List<String> calls = new ArrayList<>();
        final RecordingTriggerLogger logger = new RecordingTriggerLogger();
        final DurableTarget target = new DurableTarget(calls);
        final TriggerRunner runner = Triggers.newTrigger(new TriggerConfig<String>(DEFINITION, BINDING,
                (context, sourceTarget) -> {
                    TriggerDelivery.prepareTriggerDelivery(context, sourceTarget, EVENT);
                    sourceTarget.handleTrigger(context, EVENT);
                }, target, logger));
        runner.run(null);
        assertEquals(Arrays.asList("replay", "prepare Ev1", "handle Ev1"), calls);
        final RecordingTriggerLogger.Record record = logger.records().get(0);
        assertEquals("trigger event skipped: undeliverable", record.message);
        assertEquals("sample", record.attributes.get("connector"));
        assertEquals("main", record.attributes.get("connection"));
        assertEquals("itemCreated", record.attributes.get("trigger"));
        assertEquals("orders", record.attributes.get("binding"));
        assertEquals("Ev1", record.attributes.get("event_id"));
        assertTrue(TriggerDelivery.hasDurableTriggerInbox(target));
    }

    @Test
    void runnerWrapsReplayFailuresIncludingAborts() {
        final DurableTarget target = new DurableTarget(new ArrayList<>());
        target.replayFailure = new TriggerDeliveryAbortedException();
        final TriggerRunner runner = Triggers.newTrigger(new TriggerConfig<String>(DEFINITION, BINDING,
                (context, sourceTarget) -> { }, target, null));
        final IllegalStateException error = assertThrows(IllegalStateException.class, () -> runner.run(null));
        assertTrue(error.getMessage().startsWith("replay Trigger deliveries: "));
        assertTrue(TriggerDelivery.isTriggerDeliveryAborted(error));
    }

    @Test
    void rejectsMismatchedBindingsAndInvalidMetadata() {
        final TriggerBindingRef other = new TriggerBindingRef(new ConnectionRef("sample", "main"),
                new TriggerRef("sample", "itemDeleted"), "orders");
        assertThrows(IllegalArgumentException.class, () -> Triggers.newTrigger(
                new TriggerConfig<String>(DEFINITION, other, (context, target) -> { }, (context, event) -> { }, null)));
        assertThrows(IllegalArgumentException.class, () -> Triggers.newTrigger(
                new TriggerConfig<String>(DEFINITION, BINDING, null, (context, event) -> { }, null)));
        assertThrows(IllegalArgumentException.class, () -> new TriggerRef("Sample", "itemCreated").validate());
        assertThrows(IllegalArgumentException.class, () -> new TriggerRef("sample", "ItemCreated").validate());
        assertThrows(IllegalArgumentException.class, () -> new TriggerDefinition(TRIGGER, " ").validate());
        assertThrows(IllegalArgumentException.class, () -> new TriggerBindingRef(
                new ConnectionRef("sample", "main"), TRIGGER, " ").validate());
        assertThrows(IllegalArgumentException.class, () -> Triggers.mustTriggerBindingDefinition(
                new TriggerBindingDefinition(DEFINITION, "main", "", null)));
        assertSame(DEFINITION, Triggers.mustTriggerBindingDefinition(
                new TriggerBindingDefinition(DEFINITION, "main", "orders", ConnectorConfigurationUI.EMPTY))
                .getDefinition());
    }

    @Test
    void classifierAcceptsOnlyExactHandlerMarkers() {
        final Exception closed = new FlowNotActiveOrNotFoundException(Status.Code.NOT_FOUND, "closed", null);
        assertTrue(TriggerDelivery.isTriggerUndeliverable(DexTriggerTargets.classifyDexTriggerError(closed)));
        final Exception missing = new FlowNotFoundException(Status.Code.NOT_FOUND, "missing", null);
        assertTrue(TriggerDelivery.isTriggerUndeliverable(DexTriggerTargets.classifyDexTriggerError(missing)));
        assertTrue(TriggerDelivery.isTriggerUndeliverable(
                DexTriggerTargets.classifyDexTriggerError(new ValueMappingException("Cannot encode"))));
        for (String type : new String[] {UndeliverableTriggerException.ERROR_TYPE,
            UndeliverableTriggerException.class.getName()}) {
            assertTrue(TriggerDelivery.isTriggerUndeliverable(DexTriggerTargets.classifyDexTriggerError(
                    worker(type, "Trigger event is undeliverable"))), type);
            assertTrue(TriggerDelivery.isTriggerUndeliverable(DexTriggerTargets.classifyDexTriggerError(
                    worker(type, "Trigger event is undeliverable: closed order"))), type);
            assertFalse(TriggerDelivery.isTriggerUndeliverable(DexTriggerTargets.classifyDexTriggerError(
                    worker(type, "Trigger event is undeliverable!"))), type);
        }
        assertFalse(TriggerDelivery.isTriggerUndeliverable(DexTriggerTargets.classifyDexTriggerError(
                worker("ValueError", "Trigger event is undeliverable"))));
        final Exception unavailable = new DexServiceException(Status.Code.UNAVAILABLE, null, "down", null);
        assertSame(unavailable, DexTriggerTargets.classifyDexTriggerError(unavailable));
    }

    @Test
    void triggerRpcRequestIdMatchesTheGoAndTypeScriptVectors() {
        final String[][] vectors = {
            {"receiveReply", "Ev1", "trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA"},
            {"Approve", "Ev1", "trigger-rpc-NCT4ExQx0Ahaf4r71ej-1g"},
            {"send_user_message", "Ev1", "trigger-rpc-nvt7sgyIgs8au6Su_i3G_A"},
            {"send_user_message", "1710000200.000200", "trigger-rpc-jULspDTupwRV1mIcS03qUg"},
            {"ReceiveEnvelopeEvent", "evt_0123456789abcdef", "trigger-rpc-N-9RTMPf6sXlUYgoeYwLfg"},
            {"recordLedgerEntry", "café-事件-😀", "trigger-rpc-cVlFUH2QCxK31tF2KjqSUA"},
            {"receiveReply", "", "trigger-rpc-hBjdrS1VKDIhGHGRhqXIxw"},
            {"receiveReply", "a\uD800b", "trigger-rpc--DMh9mmLQmtdCEEqMlYxWA"},
            {"receiveReply", "a�b", "trigger-rpc--DMh9mmLQmtdCEEqMlYxWA"},
            {"receiveReply", "x��y", "trigger-rpc-8RndI-WQcs_0IiUPVanorg"},
        };
        for (String[] vector : vectors) {
            assertEquals(vector[2], DexTriggerTargets.triggerRpcRequestId(vector[0], vector[1]), vector[1]);
            assertTrue(vector[2].matches("trigger-rpc-[A-Za-z0-9_-]{22}"));
        }
        assertNotEquals(DexTriggerTargets.triggerRpcRequestId("receiveReplyE", "v1"),
                DexTriggerTargets.triggerRpcRequestId("receiveReply", "Ev1"));
        assertThrows(IllegalArgumentException.class, () -> DexTriggerTargets.triggerRpcRequestId("", "Ev1"));
        assertThrows(IllegalArgumentException.class, () -> DexTriggerTargets.triggerRpcRequestId("a\0b", "Ev1"));
    }

    @Test
    void cancellationCancelsChildrenAndRunsActions() {
        final TriggerCancellation parent = new TriggerCancellation();
        final TriggerCancellation child = new TriggerCancellation(parent);
        final AtomicInteger actions = new AtomicInteger();
        child.whenCancelled(actions::incrementAndGet);
        parent.cancel();
        parent.cancel();
        assertTrue(child.isCancelled());
        assertEquals(1, actions.get());
        child.whenCancelled(actions::incrementAndGet);
        assertEquals(2, actions.get());
        assertTrue(new TriggerCancellation(parent).isCancelled());
    }

    private static WorkerInvocationException worker(final String type, final String detail) {
        return new WorkerInvocationException(Status.Code.UNKNOWN, "worker failed", type, detail, "", Status.Code.UNKNOWN,
                null);
    }

    private static final class DurableTarget
            implements TriggerTarget<String>, TriggerDeliveryPreparer<String>, TriggerDeliveryReplayer {
        private final List<String> calls;
        private RuntimeException replayFailure;

        private DurableTarget(final List<String> calls) {
            this.calls = calls;
        }

        @Override
        public void handleTrigger(final TriggerContext context, final TriggerEvent<String> event) {
            calls.add("handle " + event.getId());
            throw new UndeliverableTriggerException("closed order");
        }

        @Override
        public void prepareTrigger(final TriggerContext context, final TriggerEvent<String> event) {
            calls.add("prepare " + event.getId());
        }

        @Override
        public void replayTriggerDeliveries(final TriggerContext context) {
            if (replayFailure != null) {
                throw replayFailure;
            }
            calls.add("replay");
        }
    }
}
