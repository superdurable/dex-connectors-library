// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import io.superdurable.dex.Context;
import io.superdurable.dex.Step;
import io.superdurable.dex.StepDecision;
import io.superdurable.dex.exceptions.RetryAfterException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Dex Step helpers shared by Connector Step factories and hand-written connector Steps.
 */
public final class ConnectorSteps {
    private static final int MAXIMUM_FAILURE_REASON_BYTES = 1024;
    private static final StepAttemptValues<Map<OperationRef, Integer>> CALL_OCCURRENCES =
            new StepAttemptValues<>(context -> new HashMap<OperationRef, Integer>());

    private static final StepAttemptValues<Instant> ATTEMPT_OBSERVED_AT =
            new StepAttemptValues<>(context -> Instant.now());

    private ConnectorSteps() {
    }

    /**
     * Returns when the SDK first observed the current Step attempt.
     *
     * <p>The Dex Java Context has no attempt deadline (superdurable/dex#2175), so the
     * single-dispatch Fence budgets from this instant plus the Execute timeout. Step factories and
     * {@link Call#forStep} record it at the start of Execute; a Context seen first later records
     * that later instant.
     *
     * @param context the Dex Step Context
     * @return the first observation of this attempt
     */
    public static Instant attemptObservedAt(final Context context) {
        return ATTEMPT_OBSERVED_AT.valueFor(context);
    }

    /**
     * Derives the Call ID of the next call of an operation in this Step attempt.
     *
     * <p>The first call of an operation in a Step attempt gets {@link CallId#derive}'s occurrence
     * 1; the n-th call of the same operation gets occurrence n, so several fenced writes of one
     * operation keep separate checkpoint entries. The count restarts each attempt, so the IDs stay
     * stable across retries only when the Step makes its calls in a deterministic order.
     *
     * @param context the Dex Step Context
     * @param operation the operation identity
     * @return the Call ID
     * @throws IllegalArgumentException if the Context has no Flow or Step execution ID
     */
    public static CallId deriveStepCallId(final Context context, final OperationRef operation) {
        if (context == null || isEmpty(context.getFlowId()) || isEmpty(context.getStepExecutionId())) {
            throw new IllegalArgumentException("connector operations require a Dex Flow and Step execution");
        }
        operation.validate();
        attemptObservedAt(context);
        final Map<OperationRef, Integer> counts = CALL_OCCURRENCES.valueFor(context);
        final int occurrence;
        synchronized (counts) {
            final Integer previous = counts.get(operation);
            occurrence = previous == null ? 1 : previous + 1;
            counts.put(operation, occurrence);
        }
        return CallId.derive(context.getFlowId(), context.getStepExecutionId(), operation, occurrence);
    }

    /**
     * Throws the Dex retry signal for a connector {@link RetryException}.
     *
     * <p>A positive delay, truncated to whole seconds, becomes
     * {@link RetryAfterException#after}. Otherwise the same exception is thrown for the Step retry
     * policy, so a delay under one second uses that policy's backoff.
     *
     * @param error the connector retry
     * @return never returns; declared so callers can write {@code throw throwDexRetry(error)}
     * @throws RetryAfterException for a delay of at least one second
     * @throws RetryException otherwise
     */
    public static RuntimeException throwDexRetry(final RetryException error) {
        if (error.getRetryAfterWholeSeconds() > 0) {
            throw RetryAfterException.after(Duration.ofSeconds(error.getRetryAfterWholeSeconds()), error);
        }
        throw error;
    }

    static <R> Map<String, Class<? extends Step<R>>> validateBranches(
            final List<BranchDefinition> definitions,
            final Map<String, Class<? extends Step<R>>> targets,
            final Set<String> optionalBranches) {
        final Map<String, BranchDefinition> declared = new HashMap<>();
        if (definitions != null) {
            for (BranchDefinition branch : definitions) {
                declared.put(branch.getId(), branch);
            }
        }
        for (Map.Entry<String, Class<? extends Step<R>>> target : targets.entrySet()) {
            if (!declared.containsKey(target.getKey())) {
                throw new IllegalArgumentException("branch target is not declared by the operation");
            }
            if (target.getValue() == null) {
                throw new IllegalArgumentException("branch target '" + target.getKey() + "' must name a Step");
            }
        }
        for (BranchDefinition branch : declared.values()) {
            if (branch.isOptional()) {
                optionalBranches.add(branch.getId());
            } else if (!targets.containsKey(branch.getId())) {
                throw new IllegalArgumentException("branch target '" + branch.getId() + "' is required");
            }
        }
        return new HashMap<>(targets);
    }

    static <R> StepDecision routeBranch(
            final Map<String, Class<? extends Step<R>>> branches, final Set<String> optionalBranches,
            final String branch, final R result, final Failure failure, final Call call, final String kind) {
        final Class<? extends Step<R>> target = branches.get(branch);
        if (target != null) {
            return StepDecision.goTo(target, result);
        }
        if (optionalBranches.contains(branch)) {
            final StringBuilder reason = new StringBuilder()
                    .append(call.getOperation().getConnectorId()).append(' ')
                    .append(call.getOperation().getOperationId())
                    .append(" (connection ").append(call.getConnection().getName()).append(") selected branch ")
                    .append(branch).append(" with no target");
            if (failure != null) {
                final byte[] message = failure.getMessage().getBytes(StandardCharsets.UTF_8);
                reason.append(": ").append(failure.getKind()).append(": ")
                        .append(truncateUtf8(failure.getMessage(), MAXIMUM_FAILURE_REASON_BYTES));
                if (message.length > MAXIMUM_FAILURE_REASON_BYTES) {
                    reason.append(" (truncated)");
                }
            }
            return StepDecision.forceFail(reason.toString());
        }
        throw new IllegalStateException(kind + " result selected unconfigured branch '" + branch + "'");
    }

    private static String truncateUtf8(final String value, final int maximumBytes) {
        int bytes = 0;
        int index = 0;
        while (index < value.length()) {
            final int codePoint = value.codePointAt(index);
            final int width = new String(Character.toChars(codePoint)).getBytes(StandardCharsets.UTF_8).length;
            if (bytes + width > maximumBytes) {
                break;
            }
            bytes += width;
            index += Character.charCount(codePoint);
        }
        return value.substring(0, index);
    }

    private static boolean isEmpty(final String value) {
        return value == null || value.isEmpty();
    }
}
