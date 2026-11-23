// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import io.superdurable.dex.Attribute;
import io.superdurable.dex.BlobCache;
import io.superdurable.dex.BlobCacheConfig;
import io.superdurable.dex.Channel;
import io.superdurable.dex.Client;
import io.superdurable.dex.ClientOptions;
import io.superdurable.dex.Context;
import io.superdurable.dex.Flow;
import io.superdurable.dex.FlowResult;
import io.superdurable.dex.FlowStatus;
import io.superdurable.dex.PersistenceSchema;
import io.superdurable.dex.PullWorkerOptions;
import io.superdurable.dex.RPC;
import io.superdurable.dex.Registry;
import io.superdurable.dex.Step;
import io.superdurable.dex.StepDecision;
import io.superdurable.dex.StepList;
import io.superdurable.dex.Wait;
import io.superdurable.dex.Worker;
import io.superdurable.dex.WorkerOptions;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import java.nio.file.Files;
import java.time.Duration;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.Arrays;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;

/** Delivers Trigger events into real Flow starts and RPCs through dexcli dev. */
@Tag("dex-dev")
class TriggerFlowsIntegrationTest {
    private static final TriggerRef TRIGGER = new TriggerRef("sample", "itemCreated");
    private static final TriggerDefinition DEFINITION = new TriggerDefinition(TRIGGER, "A sample item was created");
    private static final TriggerBindingRef BINDING =
            new TriggerBindingRef(new ConnectionRef("sample", "main"), TRIGGER, "orders");

    private static TriggeredItemFlow itemFlow;
    private static ReplyCollectorFlow replyFlow;
    private static BlobCache blobCache;
    private static Worker worker;
    private static Thread workerThread;
    private static Client client;

    @BeforeAll
    static void startDex() throws Exception {
        final String serverAddress = System.getProperty("dex.test.serverAddress", "127.0.0.1:19881");
        itemFlow = new TriggeredItemFlow();
        replyFlow = new ReplyCollectorFlow();
        final Registry registry = new Registry(Arrays.<Flow<?>>asList(itemFlow, replyFlow));
        blobCache = BlobCache.open(new BlobCacheConfig(
                Files.createTempDirectory("sdkjava-trigger-blob-cache").toString(), 1L << 28));
        worker = new Worker(registry, blobCache, WorkerOptions.newBuilder()
                .serverAddress(serverAddress)
                .pull(PullWorkerOptions.newBuilder().maxConcurrentTasks(8).build())
                .build());
        final CompletableFuture<Void> workerResult = new CompletableFuture<>();
        workerThread = new Thread(() -> {
            try {
                worker.start();
                workerResult.complete(null);
            } catch (Throwable failure) {
                workerResult.completeExceptionally(failure);
            }
        }, "dex-worker");
        workerThread.start();
        worker.waitPullRegistered(Duration.ofSeconds(30));
        client = new Client(registry, blobCache, new ClientOptions(serverAddress));
    }

    @AfterAll
    static void stopDex() throws Exception {
        if (worker != null) {
            worker.close();
            workerThread.join();
        }
        if (client != null) {
            client.close();
        }
        if (blobCache != null) {
            blobCache.close();
        }
    }

    @Test
    void flowTargetStartsOnceAndTreatsADuplicateAsDelivered() throws Exception {
        final String flowId = "sdkjava-trigger-flow-" + UUID.randomUUID();
        final RecordingTriggerLogger logger = new RecordingTriggerLogger();
        final TriggerTarget<String> target = DexTriggerTargets.newDexFlowTriggerTarget(client, itemFlow,
                event -> !"ignored".equals(event.getPayload()), event -> flowId, event -> "item:" + event.getPayload(),
                logger);
        final TriggerRunner runner = Triggers.newTrigger(new TriggerConfig<String>(DEFINITION, BINDING,
                (context, sourceTarget) -> {
                    for (TriggerEvent<String> event : Arrays.asList(event("Ev1", "first"), event("Ev1", "first"),
                            event("Ev2", "ignored"))) {
                        TriggerDelivery.prepareTriggerDelivery(context, sourceTarget, event);
                        TriggerDelivery.deliverTrigger(context, sourceTarget, event, logger);
                    }
                }, target, logger));
        runner.run(null);

        final FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(45));
        assertEquals(FlowStatus.COMPLETED, result.getStatus());
        assertEquals("item:first", result.getSingleOutput(String.class));
        final List<Boolean> duplicates = new CopyOnWriteArrayList<>();
        for (RecordingTriggerLogger.Record record : logger.records()) {
            if ("trigger event delivered".equals(record.message)) {
                duplicates.add((Boolean) record.attributes.get("duplicate"));
                assertEquals("flow_start", record.attributes.get("target"));
                assertEquals(flowId, record.attributes.get("flow_id"));
            }
        }
        assertEquals(Arrays.asList(false, true), duplicates);
        assertTrue(logger.records().stream().anyMatch(record -> "trigger event skipped: filtered".equals(record.message)));
    }

    @Test
    void rpcTargetDeliversToTheResolvedFlowAndConsumesHandlerMarkers() throws Exception {
        final String flowId = "sdkjava-trigger-rpc-" + UUID.randomUUID();
        client.startFlow(replyFlow, flowId, "waiting");
        final RecordingTriggerLogger logger = new RecordingTriggerLogger();
        final AtomicInteger rejectedInvocations = new AtomicInteger();
        final TriggerTarget<String> rejecting = DexTriggerTargets.<String, ReplyCollectorFlow, String>newDexRpcTriggerTarget(
                client, ReplyCollectorFlow.class, (dexClient, flow, input) -> {
                    rejectedInvocations.incrementAndGet();
                    dexClient.invokeRPC(flow::rejectReply, input);
                }, event -> true, event -> flowId, TriggerEvent::getPayload, logger);
        TriggerDelivery.deliverTrigger(new TriggerContext(), rejecting, event("Ev0", "spam"), logger);
        assertEquals(1, rejectedInvocations.get());

        final TriggerTarget<String> target = DexTriggerTargets.<String, ReplyCollectorFlow, String>newDexRpcTriggerTarget(
                client, ReplyCollectorFlow.class, (dexClient, flow, input) -> dexClient.invokeRPC(flow::receiveReply, input),
                event -> true, event -> flowId, TriggerEvent::getPayload, logger);
        TriggerDelivery.deliverTrigger(new TriggerContext(), target, event("Ev1", "approved"), logger);

        final FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(45));
        assertEquals(FlowStatus.COMPLETED, result.getStatus());
        assertEquals("approved", result.getSingleOutput(String.class));
        assertTrue(logger.records().stream().anyMatch(record -> "trigger event skipped: undeliverable"
                .equals(record.message) && "Ev0".equals(record.attributes.get("event_id"))));

        final String missingFlowId = "sdkjava-trigger-missing-" + UUID.randomUUID();
        final TriggerTarget<String> missing = DexTriggerTargets.<String, ReplyCollectorFlow, String>newDexRpcTriggerTarget(
                client, ReplyCollectorFlow.class, (dexClient, flow, input) -> dexClient.invokeRPC(flow::receiveReply, input),
                event -> true, event -> missingFlowId, TriggerEvent::getPayload, logger);
        TriggerDelivery.deliverTrigger(new TriggerContext(), missing, event("Ev2", "late"), logger);
        assertTrue(logger.records().stream().anyMatch(record -> "trigger event skipped: undeliverable"
                .equals(record.message) && "Ev2".equals(record.attributes.get("event_id"))));
    }

    private static TriggerEvent<String> event(final String id, final String payload) {
        return new TriggerEvent<>(id, OffsetDateTime.of(2025, 10, 12, 0, 0, 0, 0, ZoneOffset.UTC), payload);
    }

    /** Completes with the Trigger input it was started with. */
    public static class TriggeredItemFlow implements Flow<String> {
        public final Attribute<String> receivedItem = Attribute.define("received-item", String.class);
        private final RecordItem recordItem = new RecordItem();

        @Override
        public StepList<String> getSteps() {
            return StepList.startStep(recordItem);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of(receivedItem);
        }

        final class RecordItem implements Step<String> {
            @Override
            public Class<String> getInputType() {
                return String.class;
            }

            @Override
            public StepDecision execute(final Context context, final String input) {
                receivedItem.set(context, input);
                return StepDecision.gracefulComplete(input);
            }
        }
    }

    /** Waits for one reply that an RPC publishes, and rejects spam with the undeliverable marker. */
    public static class ReplyCollectorFlow implements Flow<String> {
        public final Channel<String> replies = Channel.define("replies", String.class);
        private final AwaitReply awaitReply = new AwaitReply();

        @Override
        public StepList<String> getSteps() {
            return StepList.startStep(awaitReply);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of(replies);
        }

        @RPC
        public void receiveReply(final Context context, final String reply) {
            replies.publish(context, reply);
        }

        @RPC
        public void rejectReply(final Context context, final String reply) {
            throw new UndeliverableTriggerException("reply " + reply + " is not accepted");
        }

        final class AwaitReply implements Step<String> {
            @Override
            public Class<String> getInputType() {
                return String.class;
            }

            @Override
            public Wait waitFor(final Context context, final String input) {
                return Wait.until(replies.forOne());
            }

            @Override
            public StepDecision execute(final Context context, final String input) {
                return StepDecision.gracefulComplete(replies.getConditionResults(context).get(0));
            }
        }
    }
}
