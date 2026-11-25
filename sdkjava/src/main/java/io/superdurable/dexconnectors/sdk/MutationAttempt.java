// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.time.Duration;

/**
 * One provider Mutation outcome, including narrowly justified uncertainty.
 *
 * <p>Select a branch for a definite business outcome. Request a retry for an outcome that is safe
 * to repeat. Use {@link #uncertain} only when a request may have reached the provider and neither
 * safe repetition nor read-back can resolve its outcome.
 *
 * @param <T> the operation output type
 */
public final class MutationAttempt<T> {
    /** The category of a Mutation attempt. */
    public enum Kind {
        /** A definite business outcome. */
        BRANCH,
        /** A retryable failure handled by the Step retry policy. */
        RETRY,
        /** An unconfirmable, possibly accepted write. */
        UNCERTAIN
    }

    private final Kind kind;
    private final String branch;
    private final T value;
    private final Receipt receipt;
    private final Failure failure;
    private final Duration retryAfter;

    private MutationAttempt(
            final Kind kind, final String branch, final T value, final Receipt receipt,
            final Failure failure, final Duration retryAfter) {
        this.kind = kind;
        this.branch = branch;
        this.value = value;
        this.receipt = receipt;
        this.failure = failure;
        this.retryAfter = retryAfter;
    }

    /**
     * Constructs a definite Mutation business outcome without a failure.
     *
     * @param branch the declared branch, never {@code uncertain}
     * @param value the operation output
     * @param receipt the provider receipt
     * @param <T> the operation output type
     * @return the branch attempt
     */
    public static <T> MutationAttempt<T> branch(final String branch, final T value, final Receipt receipt) {
        return branch(branch, value, receipt, null);
    }

    /**
     * Constructs a definite Mutation business outcome.
     *
     * @param branch the declared branch, never {@code uncertain}
     * @param value the operation output
     * @param receipt the provider receipt
     * @param failure the secret-safe failure of a non-happy branch, or {@code null}
     * @param <T> the operation output type
     * @return the branch attempt
     * @throws IllegalArgumentException if the branch or receipt is invalid
     */
    public static <T> MutationAttempt<T> branch(
            final String branch, final T value, final Receipt receipt, final Failure failure) {
        if (branch == null || branch.isEmpty() || receipt == null) {
            throw new IllegalArgumentException("Mutation terminal outcome requires route and receipt");
        }
        if (ConnectorBranches.UNCERTAIN.equals(branch)) {
            throw new IllegalArgumentException("uncertainty requires an uncertain Mutation attempt");
        }
        return new MutationAttempt<T>(Kind.BRANCH, branch, value, receipt, failure, Duration.ZERO);
    }

    /**
     * Constructs a retryable Mutation failure using the Step retry policy backoff.
     *
     * @param failure the secret-safe failure
     * @param <T> the operation output type
     * @return the retry attempt
     */
    public static <T> MutationAttempt<T> retry(final Failure failure) {
        return retry(failure, Duration.ZERO);
    }

    /**
     * Constructs a retryable Mutation failure with a provider-requested delay.
     *
     * @param failure the secret-safe failure
     * @param retryAfter the nonnegative delay; Dex honors only whole seconds
     * @param <T> the operation output type
     * @return the retry attempt
     * @throws IllegalArgumentException if the failure is missing or the delay is negative
     */
    public static <T> MutationAttempt<T> retry(final Failure failure, final Duration retryAfter) {
        if (failure == null || retryAfter == null || retryAfter.isNegative()) {
            throw new IllegalArgumentException("Mutation retry requires failure and nonnegative delay");
        }
        return new MutationAttempt<T>(Kind.RETRY, "", null, null, failure, retryAfter);
    }

    /**
     * Records an unconfirmable, possibly accepted, non-repeatable write.
     *
     * @param value the operation output
     * @param failure the secret-safe reason the outcome is unknown
     * @param receipt the provider receipt
     * @param <T> the operation output type
     * @return the uncertain attempt
     * @throws IllegalArgumentException if the failure or receipt is missing
     */
    public static <T> MutationAttempt<T> uncertain(final T value, final Failure failure, final Receipt receipt) {
        if (failure == null || receipt == null) {
            throw new IllegalArgumentException("uncertain Mutation requires failure and uncertainty route");
        }
        return new MutationAttempt<T>(Kind.UNCERTAIN, ConnectorBranches.UNCERTAIN, value, receipt, failure,
                Duration.ZERO);
    }

    /**
     * Returns the attempt category.
     *
     * @return the kind
     */
    public Kind getKind() {
        return kind;
    }

    /**
     * Returns the selected branch.
     *
     * @return the branch, or empty for a retry
     */
    public String getBranch() {
        return branch;
    }

    /**
     * Returns the operation output.
     *
     * @return the value, or {@code null}
     */
    public T getValue() {
        return value;
    }

    /**
     * Returns the provider receipt.
     *
     * @return the receipt, or {@code null} for a retry
     */
    public Receipt getReceipt() {
        return receipt;
    }

    /**
     * Returns the secret-safe failure.
     *
     * @return the failure, or {@code null}
     */
    public Failure getFailure() {
        return failure;
    }

    /**
     * Returns the provider-requested retry delay.
     *
     * @return the delay, zero for a terminal attempt
     */
    public Duration getRetryAfter() {
        return retryAfter;
    }
}
