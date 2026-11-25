// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import java.time.Duration;

/**
 * Retryable connector failure that a Step factory maps to a Dex retry.
 *
 * <p>{@link ConnectorOperations#runQuery} and {@link ConnectorOperations#runMutation} throw it for
 * a {@code retry} attempt. A Step that calls them directly passes it to
 * {@link ConnectorSteps#throwDexRetry(RetryException)}. Exhausting the Step retry policy fails the
 * Flow; a retry is never persisted as a terminal result.
 */
public final class RetryException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    private final transient Failure failure;
    private final Duration retryAfter;

    /**
     * Creates a retryable failure with no requested delay.
     *
     * @param failure the secret-safe failure
     */
    public RetryException(final Failure failure) {
        this(failure, Duration.ZERO);
    }

    /**
     * Creates a retryable failure with a provider-requested delay.
     *
     * @param failure the secret-safe failure
     * @param retryAfter the nonnegative delay; Dex honors only whole seconds
     * @throws IllegalArgumentException if the failure is null or the delay is null or negative
     */
    public RetryException(final Failure failure, final Duration retryAfter) {
        super(message(failure));
        if (retryAfter == null || retryAfter.isNegative()) {
            throw new IllegalArgumentException("retry delay must be finite and nonnegative");
        }
        this.failure = failure;
        this.retryAfter = retryAfter;
    }

    /**
     * Returns the secret-safe failure.
     *
     * @return the failure
     */
    public Failure getFailure() {
        return failure;
    }

    /**
     * Returns the requested delay before the next attempt.
     *
     * @return the nonnegative delay
     */
    public Duration getRetryAfter() {
        return retryAfter;
    }

    /**
     * Returns the whole-second delay that Dex can serialize, truncating any fraction.
     *
     * @return the whole seconds of {@link #getRetryAfter()}
     */
    public long getRetryAfterWholeSeconds() {
        return retryAfter.getSeconds();
    }

    private static String message(final Failure failure) {
        if (failure == null) {
            throw new IllegalArgumentException("retry failure is required");
        }
        return "connector " + failure.getProvider() + " " + failure.getOperation() + " retry ("
                + failure.getKind() + "): " + failure.getMessage();
    }
}
