// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.Mutation;
import io.superdurable.dexconnectors.sdk.MutationAttempt;
import io.superdurable.dexconnectors.sdk.MutationDefinition;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;
import java.util.function.Predicate;

/**
 * Scripted Mutation with provider-like replay of terminal keyed outcomes.
 *
 * <p>The idempotency key is the stable Call ID, so a Dex retry after a terminal answer replays
 * that answer, as a deduplicating provider would.
 *
 * @param <I> the operation input type
 * @param <O> the operation output type
 */
public final class ScriptedMutation<I, O> implements Mutation<I, O> {
    private final MutationDefinition definition;
    private final ScriptedOperation<I, O> scripted;

    /**
     * Creates a scripted Mutation.
     *
     * @param definition the connector's Mutation definition
     * @param reportFailure receives each failure message, or {@code null}
     */
    public ScriptedMutation(final MutationDefinition definition, final Consumer<String> reportFailure) {
        definition.validate();
        this.definition = definition;
        this.scripted = new ScriptedOperation<I, O>(definition.getOperation(),
                ScriptedQuery.branchIds(definition.getBranches()), true, reportFailure);
    }

    /**
     * Creates a scripted Mutation without a failure callback.
     *
     * @param definition the connector's Mutation definition
     */
    public ScriptedMutation(final MutationDefinition definition) {
        this(definition, null);
    }

    /**
     * Returns the connector's real Mutation definition.
     *
     * @return the definition
     */
    @Override
    public MutationDefinition definition() {
        return definition;
    }

    /**
     * Uses the stable Call ID as the mock provider's deduplication key.
     *
     * @param callId the Call ID
     * @param input the operation input
     * @return the Call ID text
     */
    @Override
    public String idempotencyKey(final CallId callId, final I input) {
        return callId.value();
    }

    /**
     * Answers one attempt or replays its first terminal keyed outcome.
     *
     * @param call the call, bound to a live Dex Step Context
     * @param input the operation input
     * @return the scripted attempt
     * @throws IllegalStateException if the Call has no Dex Step Context
     */
    @Override
    public MutationAttempt<O> invoke(final Call call, final I input) {
        if (call.getContext() == null) {
            throw new IllegalStateException("connector mock requires a live Dex Step context");
        }
        final MockCase<O> resolved = scripted.answer(call.getContext().getFlowId(), call.getId().value(),
                call.getConnection(), input, call.getIdempotencyKey(), 1);
        if (resolved.getKind() == MockCase.Kind.RETRY) {
            return MutationAttempt.retry(resolved.getFailure(), resolved.getRetryAfter());
        }
        if (resolved.getKind() == MockCase.Kind.UNCERTAIN) {
            return MutationAttempt.uncertain(resolved.getValue(), resolved.getFailure(), MockReceipts.forCall(call));
        }
        return MutationAttempt.branch(resolved.getBranch(), resolved.getValue(), MockReceipts.forCall(call),
                resolved.getFailure());
    }

    /**
     * Returns the underlying script for direct answers and assertions.
     *
     * @return the scripted operation
     */
    public ScriptedOperation<I, O> scripted() {
        return scripted;
    }

    /**
     * Queues the next provider outcomes in call order.
     *
     * @param cases the cases
     * @return this mock
     */
    @SafeVarargs
    public final ScriptedMutation<I, O> respond(final MockCase<O>... cases) {
        final List<MockCase<O>> copied = new ArrayList<>();
        for (MockCase<O> response : cases) {
            copied.add(response);
        }
        scripted.respondAll(copied);
        return this;
    }

    /**
     * Answers each matching input with a reusable case.
     *
     * @param predicate the input predicate
     * @param response the case
     * @return this mock
     */
    public ScriptedMutation<I, O> when(final Predicate<I> predicate, final MockCase<O> response) {
        scripted.when(predicate, response);
        return this;
    }

    /**
     * Sets fallback cases; the final case repeats after exhaustion.
     *
     * @param cases the fallback cases
     * @return this mock
     */
    @SafeVarargs
    public final ScriptedMutation<I, O> defaults(final MockCase<O>... cases) {
        final List<MockCase<O>> copied = new ArrayList<>();
        for (MockCase<O> response : cases) {
            copied.add(response);
        }
        scripted.defaultsAll(copied);
        return this;
    }

    /**
     * Requires every provider call to match an explicit script.
     *
     * @return this mock
     */
    public ScriptedMutation<I, O> strict() {
        scripted.strict();
        return this;
    }

    /**
     * Returns the script for a Flow ID or prefix.
     *
     * @param flowIdOrPrefix the Flow ID or prefix
     * @return the Flow script
     */
    public FlowScript<I, O> forFlow(final String flowIdOrPrefix) {
        return scripted.forFlow(flowIdOrPrefix);
    }

    /**
     * Returns answered calls, including replays and retries, in order.
     *
     * @return the calls
     */
    public List<RecordedCall<I>> calls() {
        return scripted.calls();
    }

    /**
     * Fails a consuming test after unscripted calls or report errors.
     */
    public void assertNoFailures() {
        scripted.assertNoFailures();
    }
}
