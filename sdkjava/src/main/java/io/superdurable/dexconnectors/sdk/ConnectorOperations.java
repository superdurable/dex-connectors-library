// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import io.superdurable.dex.Context;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * Runs provider operations once and validates their terminal outcome, independent of Worker
 * registration.
 *
 * <p>Step factories call these methods. A hand-written Step may call them directly with a Call
 * from {@link Call#forStep}, then translate a {@link RetryException} with
 * {@link ConnectorSteps#throwDexRetry(RetryException)}.
 */
public final class ConnectorOperations {
    private static final StepAttemptValues<Set<OperationRef>> RUNNING_MUTATIONS =
            new StepAttemptValues<>(context -> new HashSet<OperationRef>());

    private ConnectorOperations() {
    }

    /**
     * Invokes a Query once and validates its declared terminal branch and receipt.
     *
     * <p>Invalid operation output, including an {@link IllegalArgumentException} from the
     * operation, selects the {@code defect} branch with a safe local failure.
     *
     * @param operation the Query
     * @param call the call identity
     * @param input the operation input
     * @param <I> the operation input type
     * @param <O> the operation output type
     * @return the validated result
     * @throws RetryException when the operation requests a retry
     * @throws InterruptedException if Dex cancels the attempt
     */
    public static <I, O> QueryResult<O> runQuery(final Query<I, O> operation, final Call call, final I input)
            throws InterruptedException {
        final QueryDefinition definition = operation.definition();
        try {
            definition.validate();
            validateCall(call, definition.getOperation());
        } catch (IllegalArgumentException error) {
            return queryDefect(call, error.getMessage());
        }
        final QueryAttempt<O> attempt;
        try {
            attempt = operation.invoke(call, input);
        } catch (IllegalArgumentException error) {
            return queryDefect(call, "query returned an invalid attempt");
        }
        if (attempt == null) {
            return queryDefect(call, "query returned an invalid attempt");
        }
        if (attempt.isRetry()) {
            throw new RetryException(attempt.getFailure(), attempt.getRetryAfter());
        }
        if (!hasBranch(definition.getBranches(), attempt.getBranch())) {
            return queryDefect(call, "query returned an unknown branch");
        }
        final Receipt receipt;
        try {
            receipt = completeReceipt(attempt.getReceipt(), call);
        } catch (IllegalArgumentException error) {
            return queryDefect(call, error.getMessage());
        }
        return new QueryResult<O>(attempt.getBranch(), attempt.getValue(), receipt, attempt.getFailure());
    }

    /**
     * Invokes a Mutation once with stable idempotency and validates its outcome.
     *
     * <p>The Call ID is the fallback idempotency key. Only a declared {@code uncertain} route may
     * represent an unconfirmable, non-repeatable write. Concurrent calls of the same operation in
     * one Step attempt are unsupported: a call that starts while an earlier one still runs selects
     * {@code defect} without invoking the operation, so it never sends.
     *
     * @param operation the Mutation
     * @param call the call identity
     * @param input the operation input
     * @param <I> the operation input type
     * @param <O> the operation output type
     * @return the validated result
     * @throws RetryException when the operation requests a retry
     * @throws InterruptedException if Dex cancels the attempt
     */
    public static <I, O> MutationResult<O> runMutation(final Mutation<I, O> operation, final Call call, final I input)
            throws InterruptedException {
        final MutationDefinition definition = operation.definition();
        try {
            definition.validate();
            validateCall(call, definition.getOperation());
        } catch (IllegalArgumentException error) {
            return mutationDefect(call, error.getMessage());
        }
        final Context context = call.getContext();
        if (context == null) {
            return runValidatedMutation(operation, definition, call, input);
        }
        final Set<OperationRef> running = RUNNING_MUTATIONS.valueFor(context);
        synchronized (running) {
            if (!running.add(definition.getOperation())) {
                return mutationDefect(call, "concurrent calls of connector operation "
                        + definition.getOperation().getConnectorId() + " "
                        + definition.getOperation().getOperationId()
                        + " in one Step are not supported; an earlier call of it is still running, so this "
                        + "call was not sent");
            }
        }
        try {
            return runValidatedMutation(operation, definition, call, input);
        } finally {
            synchronized (running) {
                running.remove(definition.getOperation());
            }
        }
    }

    private static <I, O> MutationResult<O> runValidatedMutation(
            final Mutation<I, O> operation, final MutationDefinition definition, final Call call, final I input)
            throws InterruptedException {
        String key = operation.idempotencyKey(call.getId(), input);
        if (key == null || key.isEmpty()) {
            key = call.getId().value();
        }
        if (key.trim().isEmpty()) {
            return mutationDefect(call, "mutation returned an invalid idempotency key");
        }
        final Call keyedCall = call.withIdempotencyKey(key);
        final MutationAttempt<O> attempt;
        try {
            attempt = operation.invoke(keyedCall, input);
        } catch (IllegalArgumentException error) {
            return mutationDefect(keyedCall, "mutation returned an invalid attempt");
        }
        if (attempt == null) {
            return mutationDefect(keyedCall, "mutation returned an invalid attempt");
        }
        if (attempt.getKind() == MutationAttempt.Kind.RETRY) {
            throw new RetryException(attempt.getFailure(), attempt.getRetryAfter());
        }
        if (!hasBranch(definition.getBranches(), attempt.getBranch())) {
            return mutationDefect(keyedCall, "mutation returned an unknown branch");
        }
        final Receipt receipt;
        try {
            receipt = completeReceipt(attempt.getReceipt(), keyedCall);
        } catch (IllegalArgumentException error) {
            return mutationDefect(keyedCall, error.getMessage());
        }
        return new MutationResult<O>(attempt.getBranch(), attempt.getValue(), receipt, attempt.getFailure());
    }

    private static boolean hasBranch(final List<BranchDefinition> branches, final String branch) {
        if (branches == null) {
            return false;
        }
        for (BranchDefinition definition : branches) {
            if (definition.getId().equals(branch)) {
                return true;
            }
        }
        return false;
    }

    private static void validateCall(final Call call, final OperationRef operation) {
        call.getId().validate();
        call.getConnection().validate();
        if (!call.getOperation().equals(operation)) {
            throw new IllegalArgumentException("call operation does not match the definition");
        }
    }

    static Receipt completeReceipt(final Receipt receipt, final Call call) {
        if (receipt == null) {
            throw new IllegalArgumentException("operation returned no receipt");
        }
        if (!receipt.getCallId().isEmpty() && !receipt.getCallId().equals(call.getId())) {
            throw new IllegalArgumentException("operation returned a receipt for another call");
        }
        if (!receipt.getIdempotencyKey().isEmpty() && !receipt.getIdempotencyKey().equals(call.getIdempotencyKey())) {
            throw new IllegalArgumentException("operation returned a receipt with another idempotency key");
        }
        return receipt.toBuilder()
                .callId(call.getId())
                .provider(receipt.getProvider().isEmpty() ? call.getOperation().getConnectorId() : receipt.getProvider())
                .idempotencyKey(call.getIdempotencyKey())
                .observedAt(receipt.getObservedAt().isZero() ? Rfc3339NanoTimestamp.now() : receipt.getObservedAt())
                .build();
    }

    private static Failure localDefect(final Call call, final String message) {
        return new Failure(FailureKind.LOCAL_DEFECT, call.getOperation().getConnectorId(),
                call.getOperation().getOperationId(), message == null || message.isEmpty() ? "local defect" : message);
    }

    private static Receipt defectReceipt(final Call call) {
        return Receipt.newBuilder()
                .callId(call.getId())
                .provider(call.getOperation().getConnectorId())
                .observedAt(Rfc3339NanoTimestamp.now())
                .idempotencyKey(call.getIdempotencyKey())
                .build();
    }

    private static <O> QueryResult<O> queryDefect(final Call call, final String message) {
        return new QueryResult<O>(ConnectorBranches.DEFECT, null, defectReceipt(call), localDefect(call, message));
    }

    private static <O> MutationResult<O> mutationDefect(final Call call, final String message) {
        return new MutationResult<O>(ConnectorBranches.DEFECT, null, defectReceipt(call), localDefect(call, message));
    }
}
