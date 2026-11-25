// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.integration;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;

import io.superdurable.dex.BlobCache;
import io.superdurable.dex.BlobCacheConfig;
import io.superdurable.dex.Client;
import io.superdurable.dex.ClientOptions;
import io.superdurable.dex.Context;
import io.superdurable.dex.Flow;
import io.superdurable.dex.FlowResult;
import io.superdurable.dex.FlowStatus;
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
import io.superdurable.dexconnectors.sdk.ConnectorRetryPolicy;
import io.superdurable.dexconnectors.sdk.Failure;
import io.superdurable.dexconnectors.sdk.FailureKind;
import io.superdurable.dexconnectors.sdk.Mutation;
import io.superdurable.dexconnectors.sdk.MutationAttempt;
import io.superdurable.dexconnectors.sdk.MutationDefinition;
import io.superdurable.dexconnectors.sdk.MutationStep;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.Query;
import io.superdurable.dexconnectors.sdk.QueryAttempt;
import io.superdurable.dexconnectors.sdk.QueryDefinition;
import io.superdurable.dexconnectors.sdk.QueryStep;
import io.superdurable.dexconnectors.sdk.Receipt;
import io.superdurable.dexconnectors.sdk.RepeatOnUnknown;
import io.superdurable.dexconnectors.sdk.StepAnnotations;
import io.superdurable.dexconnectors.sdk.StepDefaults;
import io.superdurable.dexconnectors.sdk.StepOptionsOverride;
import io.superdurable.dexconnectors.sdk.integration.ConnectorStepsDexIntegrationTest.ItemMutationResult;
import io.superdurable.dexconnectors.sdk.integration.ConnectorStepsDexIntegrationTest.ItemQueryResult;
import io.superdurable.dexconnectors.sdk.singledispatch.DecisionKind;
import io.superdurable.dexconnectors.sdk.singledispatch.DispatchDecision;
import io.superdurable.dexconnectors.sdk.singledispatch.DispatchState;
import io.superdurable.dexconnectors.sdk.singledispatch.Fence;
import io.superdurable.dexconnectors.sdk.singledispatch.Marker;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.Files;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;

/** Proves single-dispatch checkpoints across real Dex Execute retries of one Step. */
@Tag("dex-dev")
class FenceRetryDexIntegrationTest {
    private static final OperationRef CREATE_ITEM = new OperationRef("sample", "createItem");
    private static final ConnectionRef CONNECTION = new ConnectionRef("sample", "main");
    private static final StepAnnotations ANNOTATIONS =
            new StepAnnotations("sample", "Sample", "Creates one sample item.");
    private static final Failure NOT_SENT = new Failure(FailureKind.TRANSPORT, "sample", "createItem", "not sent");
    private static final Failure UNCONFIRMED =
            new Failure(FailureKind.TRANSPORT, "sample", "createItem", "write outcome cannot be confirmed");

    private static FencedMutation lostUnsafe;
    private static FencedMutation lostBounded;
    private static FencedMutation canResend;
    private static AlwaysRetryQuery alwaysRetry;
    private static FenceRetryFlow flow;
    private static Worker worker;
    private static Thread workerThread;
    private static Client client;
    private static BlobCache blobCache;

    @BeforeAll
    static void startDex() throws Exception {
        lostUnsafe = new FencedMutation(FencedMutation.Mode.LOSE_RESPONSE, null, 0);
        lostBounded = new FencedMutation(FencedMutation.Mode.LOSE_RESPONSE, RepeatOnUnknown.LIMITED, 2);
        canResend = new FencedMutation(FencedMutation.Mode.FAIL_BEFORE_SEND_ONCE, null, 0);
        alwaysRetry = new AlwaysRetryQuery();
        flow = new FenceRetryFlow();
        final String serverAddress = System.getProperty("dex.test.serverAddress", "127.0.0.1:19881");
        final Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        blobCache = BlobCache.open(new BlobCacheConfig(
                Files.createTempDirectory("sdkjava-fence-cache").toString(), 1L << 26));
        worker = new Worker(registry, blobCache, WorkerOptions.newBuilder().serverAddress(serverAddress).build());
        workerThread = new Thread(worker::start, "sdkjava-fence-worker");
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
    void lostResponseIsReadBackAndNeverSentTwice() {
        final String flowId = "sdkjava-lost-" + UUID.randomUUID();
        assertEquals("uncertain", run(flowId, "lostUnsafe"));
        final FlowRecord record = lostUnsafe.recordFor(flowId);
        assertEquals(1, record.providerSends);
        assertEquals(1, record.readBacks);
        assertEquals(Arrays.asList(1, 1), record.callMaxSends);
        final Marker marker = record.retryMarker;
        assertNotNull(marker);
        assertEquals(DispatchState.CLAIMED, marker.getState());
        assertEquals(1, marker.getAttempt());
        assertEquals(1, marker.getSendCount());
    }

    @Test
    void boundedResendStopsAtTheStepSendCap() {
        final String flowId = "sdkjava-bounded-" + UUID.randomUUID();
        assertEquals("uncertain", run(flowId, "lostBounded"));
        final FlowRecord record = lostBounded.recordFor(flowId);
        assertEquals(Arrays.asList(3, 3, 3, 3), record.callMaxSends);
        assertEquals(3, record.providerSends);
        assertEquals(2, record.inconclusiveReadBacks);
        assertEquals(1, record.readBacks);
        assertEquals(DispatchState.CLAIMED, record.retryMarker.getState());
        assertEquals(3, record.retryMarker.getAttempt());
        assertEquals(3, record.retryMarker.getSendCount());
    }

    @Test
    void canResendCheckpointAllowsOneSendOnRetry() {
        final String flowId = "sdkjava-can-resend-" + UUID.randomUUID();
        assertEquals("created", run(flowId, "canResend"));
        final FlowRecord record = canResend.recordFor(flowId);
        assertEquals(1, record.providerSends);
        assertEquals(DispatchState.CAN_RESEND, record.retryMarker.getState());
        assertEquals(1, record.retryMarker.getAttempt());
    }

    @Test
    void retryExhaustionFailsTheFlow() {
        final String flowId = "sdkjava-exhausted-" + UUID.randomUUID();
        client.startFlow(flow, flowId, "alwaysRetry");
        final FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(90));
        assertEquals(FlowStatus.FAILED, result.getStatus());
        assertEquals(2, alwaysRetry.callsFor(flowId));
    }

    private static String run(final String flowId, final String route) {
        client.startFlow(flow, flowId, route);
        final FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(90));
        assertEquals(FlowStatus.COMPLETED, result.getStatus(), String.valueOf(result.getErrorMessage()));
        return result.getSingleOutput(String.class);
    }

    private static StepOptionsOverride retries(final int maximumAttempts) {
        return StepOptionsOverride.newBuilder()
                .executeRetry(new ConnectorRetryPolicy(Duration.ofSeconds(1), 1, Duration.ofSeconds(1),
                        maximumAttempts, Duration.ofMinutes(1)))
                .build();
    }

    /** One Flow whose start Step routes to each scenario's own Step class. */
    public static class FenceRetryFlow implements Flow<String> {
        private final RouteScenario routeScenario = new RouteScenario(this);
        private final ItemCreated itemCreated = new ItemCreated();
        private final ItemUncertain itemUncertain = new ItemUncertain();
        private final QueryStep<String, String, String, ItemQueryResult> readAlwaysRetrying;
        private final MutationStep<String, String, String, ItemMutationResult> createLostUnsafe;
        private final MutationStep<String, String, String, ItemMutationResult> createLostBounded;
        private final MutationStep<String, String, String, ItemMutationResult> createAfterCanResend;

        FenceRetryFlow() {
            readAlwaysRetrying = new QueryStep<String, String, String, ItemQueryResult>(
                    QueryStep.<String, String, String, ItemQueryResult>newConfig("readAlwaysRetrying", String.class,
                            ANNOTATIONS, alwaysRetry, CONNECTION, input -> input, ItemQueryResult::new)
                            .branch("found", ItemFound.class)
                            .stepOptionsOverride(retries(2))
                            .build()) { };
            createLostUnsafe = mutationStep("createLostUnsafe", lostUnsafe, 3, null);
            createLostBounded = mutationStep("createLostBounded", lostBounded, 5, 3);
            createAfterCanResend = mutationStep("createAfterCanResend", canResend, 3, null);
        }

        private static MutationStep<String, String, String, ItemMutationResult> mutationStep(
                final String stepType, final FencedMutation operation, final int maximumAttempts,
                final Integer maxSendsOverride) {
            final MutationStep.ConfigBuilder<String, String, String, ItemMutationResult> config =
                    MutationStep.<String, String, String, ItemMutationResult>newConfig(stepType, String.class,
                            ANNOTATIONS, operation, CONNECTION, input -> input, ItemMutationResult::new)
                            .branch("created", ItemCreated.class)
                            // The uncertain target records a write that needs manual reconciliation.
                            .branch("uncertain", ItemUncertain.class)
                            .stepOptionsOverride(retries(maximumAttempts));
            if (maxSendsOverride != null) {
                config.maxSendsOverride(maxSendsOverride);
            }
            switch (stepType) {
                case "createLostUnsafe":
                    return new MutationStep<String, String, String, ItemMutationResult>(config.build()) { };
                case "createLostBounded":
                    return new MutationStep<String, String, String, ItemMutationResult>(config.build()) { };
                default:
                    return new MutationStep<String, String, String, ItemMutationResult>(config.build()) { };
            }
        }

        @Override
        public StepList<String> getSteps() {
            return StepList.startStep(routeScenario).otherSteps(readAlwaysRetrying, createLostUnsafe,
                    createLostBounded, createAfterCanResend, itemCreated, itemUncertain, new ItemFound());
        }
    }

    /** Routes a start input to its scenario Step. */
    public static final class RouteScenario implements Step<String> {
        private final FenceRetryFlow flow;

        RouteScenario(final FenceRetryFlow flow) {
            this.flow = flow;
        }

        @Override
        public Class<String> getInputType() {
            return String.class;
        }

        @Override
        public StepDecision execute(final Context context, final String route) {
            switch (route) {
                case "alwaysRetry":
                    return StepDecision.goTo(flow.readAlwaysRetrying.stepClass(), context.getFlowId());
                case "lostUnsafe":
                    return StepDecision.goTo(flow.createLostUnsafe.stepClass(), context.getFlowId());
                case "lostBounded":
                    return StepDecision.goTo(flow.createLostBounded.stepClass(), context.getFlowId());
                default:
                    return StepDecision.goTo(flow.createAfterCanResend.stepClass(), context.getFlowId());
            }
        }
    }

    /** Completes after a created item. */
    public static final class ItemCreated implements Step<ItemMutationResult> {
        @Override
        public Class<ItemMutationResult> getInputType() {
            return ItemMutationResult.class;
        }

        @Override
        public StepDecision execute(final Context context, final ItemMutationResult input) {
            return StepDecision.forceComplete("created");
        }
    }

    /** Completes after an unconfirmable write. */
    public static final class ItemUncertain implements Step<ItemMutationResult> {
        @Override
        public Class<ItemMutationResult> getInputType() {
            return ItemMutationResult.class;
        }

        @Override
        public StepDecision execute(final Context context, final ItemMutationResult input) {
            return StepDecision.forceComplete("uncertain");
        }
    }

    /** Unreachable happy path of the always-retrying Query. */
    public static final class ItemFound implements Step<ItemQueryResult> {
        @Override
        public Class<ItemQueryResult> getInputType() {
            return ItemQueryResult.class;
        }

        @Override
        public StepDecision execute(final Context context, final ItemQueryResult input) {
            return StepDecision.forceComplete("found");
        }
    }

    /** Per-Flow observations of one fenced operation. */
    static final class FlowRecord {
        int calls;
        int providerSends;
        int readBacks;
        int inconclusiveReadBacks;
        final List<Integer> callMaxSends = new ArrayList<>();
        Marker retryMarker;
    }

    /** A fenced inert provider write that loses responses or fails before sending. */
    static final class FencedMutation implements Mutation<String, String> {
        enum Mode { LOSE_RESPONSE, FAIL_BEFORE_SEND_ONCE }

        private final Mode mode;
        private final RepeatOnUnknown tier;
        private final int maxSends;
        private final Map<String, FlowRecord> records = new ConcurrentHashMap<>();

        FencedMutation(final Mode mode, final RepeatOnUnknown tier, final int maxSends) {
            this.mode = mode;
            this.tier = tier;
            this.maxSends = maxSends;
        }

        FlowRecord recordFor(final String flowId) {
            return records.get(flowId);
        }

        @Override
        public MutationDefinition definition() {
            return new MutationDefinition(CREATE_ITEM, Arrays.asList(
                    new BranchDefinition("created", "Created"),
                    new BranchDefinition("uncertain", "Unknown outcome", true),
                    new BranchDefinition("defect", "Local defect", true)),
                    new StepDefaults(Duration.ofSeconds(30), 2), tier, maxSends);
        }

        @Override
        public String idempotencyKey(final CallId callId, final String input) {
            return "";
        }

        @Override
        public MutationAttempt<String> invoke(final Call call, final String flowId) throws InterruptedException {
            final FlowRecord record = records.computeIfAbsent(flowId, ignored -> new FlowRecord());
            record.calls++;
            record.callMaxSends.add(call.getMaxSends());
            final Fence fence = new Fence(call, Duration.ofSeconds(30), Fence.DEFAULT_WRITE_WINDOW_FRACTION,
                    call.getMaxSends());
            final DispatchDecision decision = fence.decide();
            if (decision.getMarker() != null) {
                record.retryMarker = decision.getMarker();
            }
            if (decision.getKind() == DecisionKind.READ_BACK_REQUIRED) {
                record.readBacks++;
                return MutationAttempt.uncertain(flowId, UNCONFIRMED, Receipt.empty());
            }
            if (decision.getKind() == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED) {
                // The inert provider has no read-back, which counts as inconclusive.
                record.inconclusiveReadBacks++;
            } else if (decision.getKind() != DecisionKind.SEND_ALLOWED) {
                throw new IllegalStateException("single-dispatch checkpoint is untrusted");
            }
            if (mode == Mode.FAIL_BEFORE_SEND_ONCE && record.calls == 1) {
                try {
                    fence.dispatch(writeDeadline -> {
                        throw new IOException("inert request never left the process");
                    });
                } catch (IOException notSent) {
                    fence.recordCanResend();
                    return MutationAttempt.retry(NOT_SENT);
                }
            }
            try {
                final String providerId = fence.dispatch(writeDeadline -> {
                    record.providerSends++;
                    if (mode == Mode.LOSE_RESPONSE) {
                        throw new IOException("provider response was lost");
                    }
                    return "provider-item";
                });
                fence.recordProviderId(providerId);
            } catch (IOException lostResponse) {
                // An unknown outcome propagates so Dex retries the Step under its policy.
                throw new UncheckedIOException(lostResponse);
            }
            return MutationAttempt.branch("created", flowId, Receipt.empty());
        }
    }

    /** A Query that always asks Dex to retry. */
    static final class AlwaysRetryQuery implements Query<String, String> {
        private final Map<String, Integer> calls = new ConcurrentHashMap<>();

        int callsFor(final String flowId) {
            return calls.getOrDefault(flowId, 0);
        }

        @Override
        public QueryDefinition definition() {
            return new QueryDefinition(new OperationRef("sample", "getItem"), Arrays.asList(
                    new BranchDefinition("found", "Found"),
                    new BranchDefinition("defect", "Local defect", true)),
                    new StepDefaults(Duration.ofSeconds(10), 2));
        }

        @Override
        public QueryAttempt<String> invoke(final Call call, final String flowId) {
            calls.merge(flowId, 1, Integer::sum);
            return QueryAttempt.retry(new Failure(FailureKind.AVAILABILITY, "sample", "getItem", "unavailable"));
        }
    }
}
