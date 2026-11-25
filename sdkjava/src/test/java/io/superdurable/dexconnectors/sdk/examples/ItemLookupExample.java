// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.examples;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import io.superdurable.dex.Attribute;
import io.superdurable.dex.Context;
import io.superdurable.dex.Flow;
import io.superdurable.dex.PersistenceSchema;
import io.superdurable.dex.Step;
import io.superdurable.dex.StepDecision;
import io.superdurable.dex.StepList;
import io.superdurable.dexconnectors.sdk.BranchDefinition;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.Failure;
import io.superdurable.dexconnectors.sdk.FailureKind;
import io.superdurable.dexconnectors.sdk.Mutation;
import io.superdurable.dexconnectors.sdk.MutationAttempt;
import io.superdurable.dexconnectors.sdk.MutationDefinition;
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
import io.superdurable.dexconnectors.sdk.singledispatch.DispatchDecision;
import io.superdurable.dexconnectors.sdk.singledispatch.Fence;
import java.io.IOException;
import java.time.Duration;
import java.time.Instant;
import java.util.Arrays;

/** Compiled source of the README examples. */
public final class ItemLookupExample {
    private ItemLookupExample() {
    }

    /** Reads one item from the sample provider. */
    public static final class GetItem implements Query<String, String> {
        private static final OperationRef OPERATION = new OperationRef("sample", "getItem");

        @Override
        public QueryDefinition definition() {
            return new QueryDefinition(OPERATION, Arrays.asList(
                    new BranchDefinition("found", "The item exists."),
                    new BranchDefinition("missing", "The item does not exist.", true),
                    new BranchDefinition("defect", "Connector defect.", true)),
                    new StepDefaults(Duration.ofSeconds(15), 2));
        }

        @Override
        public QueryAttempt<String> invoke(final Call call, final String itemId) {
            // Call the provider here; never put credentials in the value or receipt.
            return QueryAttempt.branch("found", "item " + itemId, Receipt.empty());
        }
    }

    /** The concrete Query result a branch target receives. */
    public static final class ItemResult extends QueryResult<String> {
        /**
         * Creates the result.
         *
         * @param branch the branch
         * @param value the item
         * @param receipt the receipt
         * @param failure the failure, or {@code null}
         */
        @JsonCreator
        public ItemResult(@JsonProperty(value = "branch", required = true) final String branch,
                          @JsonProperty("value") final String value,
                          @JsonProperty(value = "receipt", required = true) final Receipt receipt,
                          @JsonProperty("failure") final Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Looks up one item and completes with it. */
    public static class ItemLookupFlow implements Flow<String> {
        /** The last lookup result. */
        public final Attribute<ItemResult> lastLookup = Attribute.define("lastLookup", ItemResult.class);
        private final StartLookup startLookup = new StartLookup(this);
        private final ReportItem reportItem = new ReportItem();
        final QueryStep<String, String, String, ItemResult> readItem;

        /**
         * Creates the Flow.
         *
         * @param getItem the Query
         */
        public ItemLookupFlow(final GetItem getItem) {
            readItem = new QueryStep<String, String, String, ItemResult>(
                    QueryStep.<String, String, String, ItemResult>newConfig(
                            "readItem", String.class,
                            new StepAnnotations("sample", "Sample", "Reads the requested item."),
                            getItem, new ConnectionRef("sample", "main"),
                            itemId -> itemId, ItemResult::new)
                            // Only the happy path is wired; an unwired optional branch fails the Flow.
                            .branch("found", ReportItem.class)
                            .resultAttribute(lastLookup)
                            .build()) { };
        }

        @Override
        public StepList<String> getSteps() {
            return StepList.startStep(startLookup).otherSteps(readItem, reportItem);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of(lastLookup);
        }
    }

    /** Routes the start input to the Query Step. */
    public static final class StartLookup implements Step<String> {
        private final ItemLookupFlow flow;

        StartLookup(final ItemLookupFlow flow) {
            this.flow = flow;
        }

        @Override
        public Class<String> getInputType() {
            return String.class;
        }

        @Override
        public StepDecision execute(final Context context, final String itemId) {
            return StepDecision.goTo(flow.readItem.stepClass(), itemId);
        }
    }

    /** Completes with the found item. */
    public static final class ReportItem implements Step<ItemResult> {
        @Override
        public Class<ItemResult> getInputType() {
            return ItemResult.class;
        }

        @Override
        public StepDecision execute(final Context context, final ItemResult result) {
            return StepDecision.forceComplete(result.getValue());
        }
    }

    /** The sample provider's write API. */
    public interface SampleItemClient {
        /**
         * Creates an item before the write deadline.
         *
         * @param item the item
         * @param idempotencyKey the provider idempotency key
         * @param writeDeadline the Fence write deadline
         * @return the provider item ID
         * @throws IOException if the request fails
         */
        String create(String item, String idempotencyKey, Instant writeDeadline) throws IOException;
    }

    /** Creates an item with bounded resend on an unknown outcome. */
    public static final class CreateItem implements Mutation<String, String> {
        private final SampleItemClient client;

        /**
         * Creates the operation.
         *
         * @param client the provider client
         */
        public CreateItem(final SampleItemClient client) {
            this.client = client;
        }

        @Override
        public MutationDefinition definition() {
            return new MutationDefinition(new OperationRef("sample", "createItem"), Arrays.asList(
                    new BranchDefinition("created", "The item was created."),
                    new BranchDefinition("uncertain", "The write outcome cannot be confirmed.", true),
                    new BranchDefinition("defect", "Connector defect.", true)),
                    new StepDefaults(Duration.ofSeconds(15), 2), RepeatOnUnknown.LIMITED, 3);
        }

        @Override
        public String idempotencyKey(final CallId callId, final String item) {
            return "";
        }

        @Override
        public MutationAttempt<String> invoke(final Call call, final String item) throws InterruptedException {
            final Fence fence = new Fence(call, Duration.ofSeconds(15), Fence.DEFAULT_WRITE_WINDOW_FRACTION,
                    call.getMaxSends());
            final DispatchDecision decision = fence.decide();
            switch (decision.getKind()) {
                case SEND_ALLOWED:
                case READ_BACK_THEN_RESEND_ALLOWED: {
                    // After READ_BACK_THEN_RESEND_ALLOWED, read back first when the provider supports it.
                    final String id;
                    try {
                        id = fence.dispatch(writeDeadline -> client.create(item, call.getIdempotencyKey(), writeDeadline));
                    } catch (IOException error) {
                        return MutationAttempt.retry(new Failure(FailureKind.TRANSPORT, "sample", "createItem",
                                "item write failed"));
                    }
                    fence.recordProviderId(id);
                    return MutationAttempt.branch("created", id, Receipt.newBuilder().providerObjectId(id).build());
                }
                default:
                    // Read back here; select uncertain only when the write cannot be confirmed.
                    return MutationAttempt.uncertain(null, new Failure(FailureKind.TRANSPORT, "sample", "createItem",
                            "item write outcome cannot be confirmed"), Receipt.empty());
            }
        }
    }
}
