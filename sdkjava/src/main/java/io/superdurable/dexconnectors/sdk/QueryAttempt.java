// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.time.Duration;

/**
 * One provider Query outcome before Step factory validation.
 *
 * <p>An attempt is either a definite business branch or a retry. It is a private in-memory
 * decision: only the validated {@link QueryResult} crosses the JSON boundary, and a retry is never
 * persisted.
 *
 * @param <T> the operation output type
 */
public final class QueryAttempt<T> {
    private final boolean isRetry;
    private final String branch;
    private final T value;
    private final Receipt receipt;
    private final Failure failure;
    private final Duration retryAfter;

    private QueryAttempt(
            final boolean isRetry, final String branch, final T value, final Receipt receipt,
            final Failure failure, final Duration retryAfter) {
        this.isRetry = isRetry;
        this.branch = branch;
        this.value = value;
        this.receipt = receipt;
        this.failure = failure;
        this.retryAfter = retryAfter;
    }

    /**
     * Constructs a definite business outcome without a failure.
     *
     * @param branch the declared branch, never {@code uncertain}
     * @param value the operation output
     * @param receipt the provider receipt
     * @param <T> the operation output type
     * @return the branch attempt
     * @throws IllegalArgumentException if the branch or receipt is invalid
     */
    public static <T> QueryAttempt<T> branch(final String branch, final T value, final Receipt receipt) {
        return branch(branch, value, receipt, null);
    }

    /**
     * Constructs a definite business outcome.
     *
     * @param branch the declared branch, never {@code uncertain}
     * @param value the operation output
     * @param receipt the provider receipt
     * @param failure the secret-safe failure of a non-happy branch, or {@code null}
     * @param <T> the operation output type
     * @return the branch attempt
     * @throws IllegalArgumentException if the branch or receipt is invalid
     */
    public static <T> QueryAttempt<T> branch(
            final String branch, final T value, final Receipt receipt, final Failure failure) {
        if (branch == null || branch.isEmpty() || ConnectorBranches.UNCERTAIN.equals(branch) || receipt == null) {
            throw new IllegalArgumentException("Query branch requires route and receipt");
        }
        return new QueryAttempt<T>(false, branch, value, receipt, failure, Duration.ZERO);
    }

    /**
     * Constructs a retryable Query failure using the Step retry policy backoff.
     *
     * @param failure the secret-safe failure
     * @param <T> the operation output type
     * @return the retry attempt
     */
    public static <T> QueryAttempt<T> retry(final Failure failure) {
        return retry(failure, Duration.ZERO);
    }

    /**
     * Constructs a retryable Query failure with a provider-requested delay.
     *
     * @param failure the secret-safe failure
     * @param retryAfter the nonnegative delay; Dex honors only whole seconds
     * @param <T> the operation output type
     * @return the retry attempt
     * @throws IllegalArgumentException if the failure is missing or the delay is negative
     */
    public static <T> QueryAttempt<T> retry(final Failure failure, final Duration retryAfter) {
        if (failure == null || retryAfter == null || retryAfter.isNegative()) {
            throw new IllegalArgumentException("Query retry requires failure and nonnegative delay");
        }
        return new QueryAttempt<T>(true, "", null, null, failure, retryAfter);
    }

    /**
     * Reports whether this attempt requests a Dex retry.
     *
     * @return {@code true} for a retry
     */
    public boolean isRetry() {
        return isRetry;
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
     * @return the delay, zero for a branch
     */
    public Duration getRetryAfter() {
        return retryAfter;
    }
}
