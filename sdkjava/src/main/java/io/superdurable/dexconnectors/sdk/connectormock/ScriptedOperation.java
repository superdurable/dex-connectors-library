// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.ConnectorBranches;
import io.superdurable.dexconnectors.sdk.Failure;
import io.superdurable.dexconnectors.sdk.FailureKind;
import io.superdurable.dexconnectors.sdk.OperationRef;
import java.io.IOException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Consumer;
import java.util.function.Predicate;

/**
 * Thread-safe scripted Query or Mutation test model, independent of Dex Step registration.
 *
 * <p>Answer precedence is: a replayed terminal result for a known idempotency key, then the
 * longest matching {@link FlowScript} prefix, then the operation script; within a script, a
 * matching {@link #when} rule wins over the queued {@link #respond} cases. Unless
 * {@link #strict()} is set, {@link #defaults} answer what remains; an unanswered call selects
 * {@code defect} and records a failure.
 *
 * @param <I> the operation input type
 * @param <O> the operation output type
 */
public final class ScriptedOperation<I, O> {
    private final OperationRef operation;
    private final Set<String> branches;
    private final boolean isMutation;
    private final Consumer<String> reportFailure;
    private final Object lock = new Object();
    private final FlowScript.Script<I, O> script;
    private final Map<String, FlowScript<I, O>> flowScripts = new LinkedHashMap<>();
    private final Map<String, MockCase<O>> terminalByKey = new HashMap<>();
    private final List<RecordedCall<I>> calls = new ArrayList<>();
    private final List<String> failures = new ArrayList<>();
    private boolean isStrict;

    /**
     * Creates a scripted operation.
     *
     * @param operation the operation identity
     * @param branches the declared branch IDs, including {@code defect}
     * @param isMutation whether the operation is a Mutation
     * @param reportFailure receives each failure message, or {@code null}
     * @throws IllegalArgumentException if the identity is invalid or {@code defect} is missing
     */
    public ScriptedOperation(
            final OperationRef operation, final List<String> branches, final boolean isMutation,
            final Consumer<String> reportFailure) {
        operation.validate();
        if (!branches.contains(ConnectorBranches.DEFECT)) {
            throw new IllegalArgumentException("mock operation must declare defect branch");
        }
        this.operation = operation;
        this.branches = Collections.unmodifiableSet(new HashSet<>(branches));
        this.isMutation = isMutation;
        this.reportFailure = reportFailure;
        this.script = new FlowScript.Script<I, O>(this);
    }

    /**
     * Queues cases for unscoped calls.
     *
     * @param cases the cases in answer order
     * @return this operation
     */
    @SafeVarargs
    public final ScriptedOperation<I, O> respond(final MockCase<O>... cases) {
        final List<MockCase<O>> copied = new ArrayList<>();
        for (MockCase<O> response : cases) {
            copied.add(response);
        }
        return respondAll(copied);
    }

    /**
     * Queues a list of cases for unscoped calls.
     *
     * @param cases the cases in answer order
     * @return this operation
     */
    public ScriptedOperation<I, O> respondAll(final List<MockCase<O>> cases) {
        validateCases(cases);
        synchronized (lock) {
            script.enqueue(cases);
        }
        return this;
    }

    /**
     * Adds an unscoped reusable input rule.
     *
     * @param predicate the input predicate
     * @param response the case
     * @return this operation
     */
    public ScriptedOperation<I, O> when(final Predicate<I> predicate, final MockCase<O> response) {
        validateCases(Arrays.asList(response));
        synchronized (lock) {
            script.addRule(predicate, response);
        }
        return this;
    }

    /**
     * Sets fallback cases, repeating the final one.
     *
     * @param cases the fallback cases
     * @return this operation
     */
    @SafeVarargs
    public final ScriptedOperation<I, O> defaults(final MockCase<O>... cases) {
        final List<MockCase<O>> copied = new ArrayList<>();
        for (MockCase<O> response : cases) {
            copied.add(response);
        }
        return defaultsAll(copied);
    }

    /**
     * Sets a list of fallback cases, repeating the final one.
     *
     * @param cases the fallback cases
     * @return this operation
     */
    public ScriptedOperation<I, O> defaultsAll(final List<MockCase<O>> cases) {
        validateCases(cases);
        synchronized (lock) {
            script.replaceDefaults(cases);
        }
        return this;
    }
    /**
     * Requires a {@link #respond} or {@link #when} answer for every call.
     *
     * @return this operation
     */
    public ScriptedOperation<I, O> strict() {
        synchronized (lock) {
            isStrict = true;
        }
        return this;
    }

    /**
     * Returns the script for a Flow ID prefix, creating it once.
     *
     * @param flowIdOrPrefix the Flow ID or prefix
     * @return the Flow script
     */
    public FlowScript<I, O> forFlow(final String flowIdOrPrefix) {
        synchronized (lock) {
            FlowScript<I, O> flowScript = flowScripts.get(flowIdOrPrefix);
            if (flowScript == null) {
                flowScript = new FlowScript<I, O>(flowIdOrPrefix, lock, this);
                flowScripts.put(flowIdOrPrefix, flowScript);
            }
            return flowScript;
        }
    }

    /**
     * Resolves script precedence and replays terminal keyed Mutation results.
     *
     * @param flowId the Dex Flow ID
     * @param callId the Call ID
     * @param connection the connection
     * @param input the operation input
     * @param idempotencyKey the idempotency key, or empty
     * @param pageNumber the one-based listing page
     * @return the resolved case, never paged
     */
    public MockCase<O> answer(
            final String flowId, final String callId, final ConnectionRef connection, final I input,
            final String idempotencyKey, final int pageNumber) {
        synchronized (lock) {
            final boolean hasKey = idempotencyKey != null && !idempotencyKey.isEmpty();
            final boolean isReplay = hasKey && terminalByKey.containsKey(idempotencyKey);
            boolean isUnscripted = false;
            MockCase<O> response;
            if (isReplay) {
                response = terminalByKey.get(idempotencyKey);
            } else {
                final List<FlowScript.Script<I, O>> scripts = matchingScripts(flowId);
                response = null;
                for (FlowScript.Script<I, O> candidate : scripts) {
                    response = candidate.answer(input);
                    if (response != null) {
                        break;
                    }
                }
                if (response == null && !isStrict) {
                    for (FlowScript.Script<I, O> candidate : scripts) {
                        response = candidate.nextDefault();
                        if (response != null) {
                            break;
                        }
                    }
                }
                if (response == null) {
                    isUnscripted = true;
                    recordFailure("unscripted call for Flow '" + flowId + "'");
                    response = defect("unscripted connector mock call");
                }
                if (response.getKind() == MockCase.Kind.PAGED) {
                    if (pageNumber < 1 || pageNumber > response.getPages().size()) {
                        recordFailure("requested page is outside the scripted listing");
                        response = defect("requested page is outside the scripted listing");
                    } else {
                        response = MockCase.branch(response.getBranch(), response.getPages().get(pageNumber - 1));
                    }
                }
                if (hasKey && response.getKind() != MockCase.Kind.RETRY) {
                    terminalByKey.put(idempotencyKey, response);
                }
            }
            final boolean isRetry = response.getKind() == MockCase.Kind.RETRY;
            final RecordedCall<I> recorded = new RecordedCall<I>(operation.getConnectorId(),
                    operation.getOperationId(), connection.getName(), flowId, callId, input,
                    isRetry ? "" : response.getBranch(), isRetry, isReplay, isUnscripted);
            calls.add(recorded);
            try {
                ConnectorMockReport.appendToReport(recorded);
            } catch (IOException error) {
                recordFailure(error.getMessage());
            }
            return response;
        }
    }

    /**
     * Returns answered calls in answer order.
     *
     * @return the calls
     */
    public List<RecordedCall<I>> calls() {
        synchronized (lock) {
            return Collections.unmodifiableList(new ArrayList<>(calls));
        }
    }

    /**
     * Returns missing scripts and report failures for test assertions.
     *
     * @return the failure messages
     */
    public List<String> failures() {
        synchronized (lock) {
            return Collections.unmodifiableList(new ArrayList<>(failures));
        }
    }

    /**
     * Fails the consuming test when mock calls were unscripted or invalid.
     *
     * @throws AssertionError listing every failure
     */
    public void assertNoFailures() {
        final List<String> current = failures();
        if (!current.isEmpty()) {
            throw new AssertionError(String.join("; ", current));
        }
    }

    void validateCases(final List<MockCase<O>> cases) {
        for (MockCase<O> response : cases) {
            if (response == null) {
                throw new IllegalArgumentException("mock case is required");
            }
            if ((response.getKind() == MockCase.Kind.BRANCH || response.getKind() == MockCase.Kind.PAGED)
                    && !branches.contains(response.getBranch())) {
                throw new IllegalArgumentException("mock branch is not declared");
            }
            if (response.getKind() == MockCase.Kind.UNCERTAIN
                    && (!isMutation || !branches.contains(ConnectorBranches.UNCERTAIN))) {
                throw new IllegalArgumentException("uncertainty requires a declared Mutation branch");
            }
            if (response.getKind() == MockCase.Kind.RETRY
                    && (response.getFailure() == null || response.getRetryAfter().isNegative())) {
                throw new IllegalArgumentException("invalid mock retry");
            }
        }
    }

    private List<FlowScript.Script<I, O>> matchingScripts(final String flowId) {
        final List<FlowScript<I, O>> matching = new ArrayList<>();
        for (FlowScript<I, O> flowScript : flowScripts.values()) {
            if (flowId.startsWith(flowScript.getPrefix())) {
                matching.add(flowScript);
            }
        }
        matching.sort((left, right) -> right.getPrefix().length() - left.getPrefix().length());
        final List<FlowScript.Script<I, O>> scripts = new ArrayList<>();
        for (FlowScript<I, O> flowScript : matching) {
            scripts.add(flowScript.script());
        }
        scripts.add(script);
        return scripts;
    }

    private MockCase<O> defect(final String message) {
        return MockCase.branch(ConnectorBranches.DEFECT, null, new Failure(FailureKind.LOCAL_DEFECT,
                operation.getConnectorId(), operation.getOperationId(), message));
    }

    private void recordFailure(final String message) {
        failures.add(message);
        if (reportFailure != null) {
            reportFailure.accept(message);
        }
    }
}
