// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import io.superdurable.dexconnectors.sdk.BranchDefinition;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.Query;
import io.superdurable.dexconnectors.sdk.QueryAttempt;
import io.superdurable.dexconnectors.sdk.QueryDefinition;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;
import java.util.function.Predicate;

/**
 * Scripted Query implementing the real operation contract for connector and application tests.
 *
 * <p>Construct it from the connector's actual definition. A live Dex Step Context supplies the
 * Flow identity for scoped scripts during invocation.
 *
 * @param <I> the operation input type
 * @param <O> the operation output type
 */
public final class ScriptedQuery<I, O> implements Query<I, O> {
    private final QueryDefinition definition;
    private final ScriptedOperation<I, O> scripted;

    /**
     * Creates a scripted Query.
     *
     * @param definition the connector's Query definition
     * @param reportFailure receives each failure message, or {@code null}
     */
    public ScriptedQuery(final QueryDefinition definition, final Consumer<String> reportFailure) {
        definition.validate();
        this.definition = definition;
        this.scripted = new ScriptedOperation<I, O>(definition.getOperation(), branchIds(definition.getBranches()),
                false, reportFailure);
    }

    /**
     * Creates a scripted Query without a failure callback.
     *
     * @param definition the connector's Query definition
     */
    public ScriptedQuery(final QueryDefinition definition) {
        this(definition, null);
    }

    /**
     * Returns the connector's real Query definition.
     *
     * @return the definition
     */
    @Override
    public QueryDefinition definition() {
        return definition;
    }

    /**
     * Answers one provider attempt using the configured script.
     *
     * @param call the call, bound to a live Dex Step Context
     * @param input the operation input
     * @return the scripted attempt
     * @throws IllegalStateException if the Call has no Dex Step Context
     */
    @Override
    public QueryAttempt<O> invoke(final Call call, final I input) {
        if (call.getContext() == null) {
            throw new IllegalStateException("connector mock requires a live Dex Step context");
        }
        final MockCase<O> resolved = scripted.answer(call.getContext().getFlowId(), call.getId().value(),
                call.getConnection(), input, "", 1);
        if (resolved.getKind() == MockCase.Kind.RETRY) {
            return QueryAttempt.retry(resolved.getFailure(), resolved.getRetryAfter());
        }
        return QueryAttempt.branch(resolved.getBranch(), resolved.getValue(), MockReceipts.forCall(call),
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
    public final ScriptedQuery<I, O> respond(final MockCase<O>... cases) {
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
    public ScriptedQuery<I, O> when(final Predicate<I> predicate, final MockCase<O> response) {
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
    public final ScriptedQuery<I, O> defaults(final MockCase<O>... cases) {
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
    public ScriptedQuery<I, O> strict() {
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
     * Returns answered calls, including retries, in order.
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

    static List<String> branchIds(final List<BranchDefinition> branches) {
        final List<String> ids = new ArrayList<>();
        if (branches != null) {
            for (BranchDefinition branch : branches) {
                ids.add(branch.getId());
            }
        }
        return ids;
    }
}
