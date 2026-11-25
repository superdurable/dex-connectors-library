// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.integration;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import io.superdurable.dex.Attribute;
import io.superdurable.dex.BlobCacheConfig;
import io.superdurable.dex.Client;
import io.superdurable.dex.ClientOptions;
import io.superdurable.dex.Context;
import io.superdurable.dex.Flow;
import io.superdurable.dex.FlowResult;
import io.superdurable.dex.FlowStatus;
import io.superdurable.dex.PersistenceSchema;
import io.superdurable.dex.Registry;
import io.superdurable.dex.Step;
import io.superdurable.dex.StepDecision;
import io.superdurable.dex.StepList;
import io.superdurable.dex.Worker;
import io.superdurable.dex.WorkerOptions;
import io.superdurable.dexconnectors.sdk.BranchDefinition;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.Failure;
import io.superdurable.dexconnectors.sdk.FailureKind;
import io.superdurable.dexconnectors.sdk.Mutation;
import io.superdurable.dexconnectors.sdk.MutationAttempt;
import io.superdurable.dexconnectors.sdk.MutationDefinition;
import io.superdurable.dexconnectors.sdk.MutationResult;
import io.superdurable.dexconnectors.sdk.MutationStep;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.Query;
import io.superdurable.dexconnectors.sdk.QueryAttempt;
import io.superdurable.dexconnectors.sdk.QueryDefinition;
import io.superdurable.dexconnectors.sdk.QueryResult;
import io.superdurable.dexconnectors.sdk.QueryStep;
import io.superdurable.dexconnectors.sdk.Receipt;
import io.superdurable.dexconnectors.sdk.RepeatOnUnknown;
import io.superdurable.dexconnectors.sdk.StepAnnotations;
import io.superdurable.dexconnectors.sdk.StepDefaults;
import io.superdurable.dexconnectors.sdk.singledispatch.DecisionKind;
import io.superdurable.dexconnectors.sdk.singledispatch.DispatchDecision;
import io.superdurable.dexconnectors.sdk.singledispatch.Fence;
import java.nio.file.Files;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;

/** Runs connector Step factories in real Dex Flows served by {@code dexcli dev}. */
@Tag("dex-dev")
class ConnectorStepsDexIntegrationTest {
    private static final OperationRef GET_ITEM = new OperationRef("sample", "getItem");
    private static final OperationRef CREATE_ITEM = new OperationRef("sample", "createItem");
    private static final ConnectionRef CONNECTION = new ConnectionRef("sample", "main");
    private static final StepAnnotations ANNOTATIONS =
            new StepAnnotations("sample", "Sample", "Calls the sample provider.");

    private static ScriptedQuery query;
    private static FencedMutation mutation;
    private static SampleConnectorFlow flow;
    private static Worker worker;
    private static Thread workerThread;
    private static Client client;
    private static io.superdurable.dex.BlobCache blobCache;

    @BeforeAll
    static void startDex() throws Exception {
        query = new ScriptedQuery();
        mutation = new FencedMutation();
        flow = new SampleConnectorFlow(query, mutation);
        final String serverAddress = System.getProperty("dex.test.serverAddress", "127.0.0.1:19881");
        final Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        blobCache = io.superdurable.dex.BlobCache.open(new BlobCacheConfig(
                Files.createTempDirectory("sdkjava-dex-cache").toString(), 1L << 26));
        worker = new Worker(registry, blobCache, WorkerOptions.newBuilder().serverAddress(serverAddress).build());
        final CompletableFuture<Void> workerResult = new CompletableFuture<>();
        workerThread = new Thread(() -> {
            try {
                worker.start();
                workerResult.complete(null);
            } catch (Throwable failure) {
                workerResult.completeExceptionally(failure);
            }
        }, "sdkjava-dex-worker");
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
    void queryRetriesThenRoutesTypedResultAndPersistsIt() {
        final String flowId = "sdkjava-query-" + UUID.randomUUID();
        query.script(flowId, "retry", "found");
        client.startFlow(flow, flowId, "query:" + flowId);
        final FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(60));
        assertEquals(FlowStatus.COMPLETED, result.getStatus(), String.valueOf(result.getErrorMessage()));
        assertEquals("item-for-" + flowId, result.getSingleOutput(String.class));
        assertEquals(2, query.callsFor(flowId));
        final ItemQueryResult stored = client.getAttributes(flowId, flow.lastQueryResult)
                .get(flow.lastQueryResult).get();
        assertEquals("found", stored.getBranch());
        assertEquals("item-for-" + flowId, stored.getValue());
        assertEquals("sample", stored.getReceipt().getProvider());
        stored.getReceipt().getCallId().validate();
    }

    @Test
    void unwiredOptionalBranchFailsTheFlow() {
        final String flowId = "sdkjava-optional-" + UUID.randomUUID();
        query.script(flowId, "alternate");
        client.startFlow(flow, flowId, "query:" + flowId);
        final FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(60));
        assertEquals(FlowStatus.FAILED, result.getStatus());
        assertTrue(String.valueOf(result.getErrorMessage()).contains("selected branch alternate with no target"),
                String.valueOf(result.getErrorMessage()));
    }

    @Test
    void twoStepsOfOneMutationEachSendOnceWithDistinctKeys() {
        final String flowId = "sdkjava-mutation-" + UUID.randomUUID();
        client.startFlow(flow, flowId, "mutation:" + flowId);
        final FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(60));
        assertEquals(FlowStatus.COMPLETED, result.getStatus(), String.valueOf(result.getErrorMessage()));
        final List<String> keys = mutation.sentKeysFor(flowId);
        assertEquals(2, keys.size());
        assertNotEquals(keys.get(0), keys.get(1));
        assertEquals(keys.get(1), result.getSingleOutput(String.class));
        assertTrue(keys.get(0).startsWith("sample/"));
    }

    /** Concrete Query result class used as a Dex Step input type. */
    public static final class ItemQueryResult extends QueryResult<String> {
        /**
         * Creates the result.
         *
         * @param branch the branch
         * @param value the value
         * @param receipt the receipt
         * @param failure the failure
         */
        @JsonCreator
        public ItemQueryResult(
                @JsonProperty(value = "branch", required = true) final String branch,
                @JsonProperty("value") final String value,
                @JsonProperty(value = "receipt", required = true) final Receipt receipt,
                @JsonProperty("failure") final Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Concrete Mutation result class used as a Dex Step input type. */
    public static final class ItemMutationResult extends MutationResult<String> {
        /**
         * Creates the result.
         *
         * @param branch the branch
         * @param value the value
         * @param receipt the receipt
         * @param failure the failure
         */
        @JsonCreator
        public ItemMutationResult(
                @JsonProperty(value = "branch", required = true) final String branch,
                @JsonProperty("value") final String value,
                @JsonProperty(value = "receipt", required = true) final Receipt receipt,
                @JsonProperty("failure") final Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** The Flow under test; each connector Step use is its own anonymous subclass. */
    public static class SampleConnectorFlow implements Flow<String> {
        final Attribute<ItemQueryResult> lastQueryResult = Attribute.define("lastQueryResult", ItemQueryResult.class);
        private final StartStep startStep = new StartStep(this);
        private final QueryStep<String, String, String, ItemQueryResult> readItem;
        private final MutationStep<String, String, String, ItemMutationResult> createFirstItem;
        private final MutationStep<String, String, String, ItemMutationResult> createSecondItem;
        private final CreateSecondItemStep createSecondItemStep = new CreateSecondItemStep(this);
        private final ItemFoundStep itemFoundStep = new ItemFoundStep();
        private final ItemsCreatedStep itemsCreatedStep = new ItemsCreatedStep();

        SampleConnectorFlow(final ScriptedQuery query, final FencedMutation mutation) {
            readItem = new QueryStep<String, String, String, ItemQueryResult>(
                    QueryStep.<String, String, String, ItemQueryResult>newConfig("readItem", String.class, ANNOTATIONS,
                            query, CONNECTION, input -> input, ItemQueryResult::new)
                            .branch("found", ItemFoundStep.class)
                            .resultAttribute(lastQueryResult)
                            .build()) { };
            createFirstItem = new MutationStep<String, String, String, ItemMutationResult>(
                    MutationStep.<String, String, String, ItemMutationResult>newConfig("createFirstItem", String.class,
                            ANNOTATIONS, mutation, CONNECTION, input -> input, ItemMutationResult::new)
                            .branch("created", CreateSecondItemStep.class)
                            .build()) { };
            createSecondItem = new MutationStep<String, String, String, ItemMutationResult>(
                    MutationStep.<String, String, String, ItemMutationResult>newConfig("createSecondItem", String.class,
                            ANNOTATIONS, mutation, CONNECTION, input -> input, ItemMutationResult::new)
                            .branch("created", ItemsCreatedStep.class)
                            .build()) { };
        }

        @Override
        public StepList<String> getSteps() {
            return StepList.startStep(startStep).otherSteps(readItem, createFirstItem, createSecondItem,
                    createSecondItemStep, itemFoundStep, itemsCreatedStep);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of(lastQueryResult);
        }
    }

    /** Routes a test input to the Query or the Mutation chain. */
    public static final class StartStep implements Step<String> {
        private final SampleConnectorFlow flow;

        StartStep(final SampleConnectorFlow flow) {
            this.flow = flow;
        }

        @Override
        public Class<String> getInputType() {
            return String.class;
        }

        @Override
        public StepDecision execute(final Context context, final String input) {
            if (input.startsWith("query:")) {
                return StepDecision.goTo(flow.readItem.stepClass(), context.getFlowId());
            }
            return StepDecision.goTo(flow.createFirstItem.stepClass(), "first");
        }
    }

    /** Starts the second Mutation Step after the first creates its item. */
    public static final class CreateSecondItemStep implements Step<ItemMutationResult> {
        private final SampleConnectorFlow flow;

        CreateSecondItemStep(final SampleConnectorFlow flow) {
            this.flow = flow;
        }

        @Override
        public Class<ItemMutationResult> getInputType() {
            return ItemMutationResult.class;
        }

        @Override
        public StepDecision execute(final Context context, final ItemMutationResult input) {
            return StepDecision.goTo(flow.createSecondItem.stepClass(), "second");
        }
    }

    /** Completes with the found item. */
    public static final class ItemFoundStep implements Step<ItemQueryResult> {
        @Override
        public Class<ItemQueryResult> getInputType() {
            return ItemQueryResult.class;
        }

        @Override
        public StepDecision execute(final Context context, final ItemQueryResult input) {
            return StepDecision.forceComplete(input.getValue());
        }
    }

    /** Completes with the second item's idempotency key. */
    public static final class ItemsCreatedStep implements Step<ItemMutationResult> {
        @Override
        public Class<ItemMutationResult> getInputType() {
            return ItemMutationResult.class;
        }

        @Override
        public StepDecision execute(final Context context, final ItemMutationResult input) {
            return StepDecision.forceComplete(input.getReceipt().getIdempotencyKey());
        }
    }

    /** Returns scripted outcomes per Flow and counts every provider call. */
    static final class ScriptedQuery implements Query<String, String> {
        private final java.util.Map<String, List<String>> outcomes = new java.util.concurrent.ConcurrentHashMap<>();
        private final java.util.Map<String, AtomicInteger> calls = new java.util.concurrent.ConcurrentHashMap<>();

        void script(final String flowId, final String... flowOutcomes) {
            outcomes.put(flowId, Arrays.asList(flowOutcomes));
            calls.put(flowId, new AtomicInteger());
        }

        int callsFor(final String flowId) {
            return calls.get(flowId).get();
        }

        @Override
        public QueryDefinition definition() {
            return new QueryDefinition(GET_ITEM, Arrays.asList(
                    new BranchDefinition("found", "Found"),
                    new BranchDefinition("alternate", "Different outcome", true),
                    new BranchDefinition("defect", "Local defect", true)),
                    new StepDefaults(Duration.ofSeconds(10), 2, Duration.ZERO,
                            new io.superdurable.dexconnectors.sdk.ConnectorRetryPolicy(
                                    Duration.ofSeconds(1), 1, Duration.ofSeconds(1), 5, Duration.ofMinutes(1))));
        }

        @Override
        public QueryAttempt<String> invoke(final Call call, final String flowId) {
            final List<String> flowOutcomes = outcomes.get(flowId);
            final int index = calls.get(flowId).getAndIncrement();
            final String outcome = flowOutcomes.get(Math.min(index, flowOutcomes.size() - 1));
            if ("retry".equals(outcome)) {
                return QueryAttempt.retry(new Failure(FailureKind.AVAILABILITY, "sample", "getItem", "unavailable"));
            }
            if ("alternate".equals(outcome)) {
                return QueryAttempt.branch("alternate", "", Receipt.empty(),
                        new Failure(FailureKind.NOT_FOUND, "sample", "getItem", "item is elsewhere"));
            }
            return QueryAttempt.branch("found", "item-for-" + flowId, Receipt.empty());
        }
    }

    /** Fences every send and records the idempotency key the provider received. */
    static final class FencedMutation implements Mutation<String, String> {
        private final java.util.Map<String, List<String>> sentKeys = new java.util.concurrent.ConcurrentHashMap<>();

        List<String> sentKeysFor(final String flowId) {
            return sentKeys.getOrDefault(flowId, Collections.<String>emptyList());
        }

        @Override
        public MutationDefinition definition() {
            return new MutationDefinition(CREATE_ITEM, Arrays.asList(
                    new BranchDefinition("created", "Created"),
                    new BranchDefinition("uncertain", "Unknown outcome", true),
                    new BranchDefinition("defect", "Local defect", true)),
                    new StepDefaults(Duration.ofSeconds(10), 2), RepeatOnUnknown.LIMITED, 3);
        }

        @Override
        public String idempotencyKey(final CallId callId, final String input) {
            return "sample/" + callId.value();
        }

        @Override
        public MutationAttempt<String> invoke(final Call call, final String input) throws InterruptedException {
            final Fence fence = new Fence(call, Duration.ofSeconds(10), Fence.DEFAULT_WRITE_WINDOW_FRACTION,
                    call.getMaxSends());
            final DispatchDecision decision = fence.decide();
            if (decision.getKind() != DecisionKind.SEND_ALLOWED) {
                return MutationAttempt.uncertain("", new Failure(FailureKind.TRANSPORT, "sample", "createItem",
                        "write outcome cannot be confirmed"), Receipt.empty());
            }
            final String flowId = call.getContext().getFlowId();
            final String key = call.getIdempotencyKey();
            try {
                fence.dispatch(writeDeadline -> {
                    sentKeys.computeIfAbsent(flowId, ignored -> Collections.synchronizedList(new ArrayList<String>()))
                            .add(key);
                    return key;
                });
            } catch (java.io.IOException error) {
                return MutationAttempt.retry(new Failure(FailureKind.TRANSPORT, "sample", "createItem", "send failed"));
            }
            fence.recordProviderId(key);
            return MutationAttempt.branch("created", key, Receipt.newBuilder().providerObjectId(key).build());
        }
    }
}
