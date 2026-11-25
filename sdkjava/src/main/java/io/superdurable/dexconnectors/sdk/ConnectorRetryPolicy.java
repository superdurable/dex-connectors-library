// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import io.superdurable.dex.RetryPolicy;
import java.time.Duration;
import java.util.Objects;

/**
 * Execute retry schedule owned by a connector operation definition.
 *
 * <p>Zero maximum attempts means unlimited attempts within the total duration. The JSON form is
 * the Go {@code dex.RetryPolicy} shape: capitalized field names and integer nanosecond durations.
 * Convert to the Dex Java SDK policy with {@link #toDexRetryPolicy(int)}.
 */
@JsonPropertyOrder({"InitialInterval", "BackoffCoefficient", "MaximumInterval", "MaximumAttempts", "TotalDuration"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class ConnectorRetryPolicy {
    private final Duration initialInterval;
    private final double backoffCoefficient;
    private final Duration maximumInterval;
    private final int maximumAttempts;
    private final Duration totalDuration;

    /**
     * Creates a validated retry policy.
     *
     * @param initialInterval the first retry delay
     * @param backoffCoefficient the multiplier applied after each attempt
     * @param maximumInterval the largest retry delay
     * @param maximumAttempts the attempt limit, or zero for unlimited
     * @param totalDuration the total retry budget
     * @throws IllegalArgumentException if any value is null or negative
     */
    public ConnectorRetryPolicy(
            final Duration initialInterval,
            final double backoffCoefficient,
            final Duration maximumInterval,
            final int maximumAttempts,
            final Duration totalDuration) {
        if (initialInterval == null || maximumInterval == null || totalDuration == null
                || initialInterval.isNegative() || maximumInterval.isNegative() || totalDuration.isNegative()
                || backoffCoefficient < 0 || Double.isNaN(backoffCoefficient) || Double.isInfinite(backoffCoefficient)
                || maximumAttempts < 0) {
            throw new IllegalArgumentException("invalid connector retry policy");
        }
        this.initialInterval = initialInterval;
        this.backoffCoefficient = backoffCoefficient;
        this.maximumInterval = maximumInterval;
        this.maximumAttempts = maximumAttempts;
        this.totalDuration = totalDuration;
    }

    /**
     * Returns a fresh one-hour policy with one-second exponential backoff capped at five minutes.
     *
     * @return the default connector Step Execute retry policy
     */
    public static ConnectorRetryPolicy defaultConnectorStepExecuteRetryPolicy() {
        return new ConnectorRetryPolicy(Duration.ofSeconds(1), 2, Duration.ofMinutes(5), 0, Duration.ofHours(1));
    }

    @JsonCreator
    static ConnectorRetryPolicy fromWire(
            @JsonProperty(value = "InitialInterval", required = true) final long initialIntervalNanos,
            @JsonProperty(value = "BackoffCoefficient", required = true) final double backoffCoefficient,
            @JsonProperty(value = "MaximumInterval", required = true) final long maximumIntervalNanos,
            @JsonProperty(value = "MaximumAttempts", required = true) final int maximumAttempts,
            @JsonProperty(value = "TotalDuration", required = true) final long totalDurationNanos) {
        return new ConnectorRetryPolicy(Duration.ofNanos(initialIntervalNanos), backoffCoefficient,
                Duration.ofNanos(maximumIntervalNanos), maximumAttempts, Duration.ofNanos(totalDurationNanos));
    }

    /**
     * Returns the first retry delay.
     *
     * @return the initial interval
     */
    public Duration getInitialInterval() {
        return initialInterval;
    }

    /**
     * Returns the multiplier applied after each attempt.
     *
     * @return the backoff coefficient
     */
    public double getBackoffCoefficient() {
        return backoffCoefficient;
    }

    /**
     * Returns the largest retry delay.
     *
     * @return the maximum interval
     */
    public Duration getMaximumInterval() {
        return maximumInterval;
    }

    /**
     * Returns the attempt limit, where zero means unlimited.
     *
     * @return the maximum attempts
     */
    public int getMaximumAttempts() {
        return maximumAttempts;
    }

    /**
     * Returns the total retry budget.
     *
     * @return the total duration
     */
    public Duration getTotalDuration() {
        return totalDuration;
    }

    @JsonProperty("InitialInterval")
    long initialIntervalNanos() {
        return initialInterval.toNanos();
    }

    // Go encodes a whole float64 without a fraction, so 2 stays "2" rather than "2.0".
    @JsonProperty("BackoffCoefficient")
    Number backoffCoefficientWire() {
        if (backoffCoefficient == Math.rint(backoffCoefficient) && Math.abs(backoffCoefficient) < 1e15) {
            return Long.valueOf((long) backoffCoefficient);
        }
        return Double.valueOf(backoffCoefficient);
    }

    @JsonProperty("MaximumInterval")
    long maximumIntervalNanos() {
        return maximumInterval.toNanos();
    }

    @JsonProperty("MaximumAttempts")
    int maximumAttemptsWire() {
        return maximumAttempts;
    }

    @JsonProperty("TotalDuration")
    long totalDurationNanos() {
        return totalDuration.toNanos();
    }

    /**
     * Converts to the Dex Java SDK retry policy.
     *
     * @param additionalAttempts attempts added to a limited policy, such as a raised send cap
     * @return the Dex retry policy
     */
    public RetryPolicy toDexRetryPolicy(final int additionalAttempts) {
        int attempts = maximumAttempts;
        if (attempts > 0) {
            // Zero means unlimited attempts, which already reach any send cap.
            attempts += Math.max(0, additionalAttempts);
        }
        return RetryPolicy.newBuilder()
                .initialInterval(initialInterval)
                .backoffCoefficient(backoffCoefficient)
                .maximumInterval(maximumInterval)
                .maximumAttempts(attempts)
                .totalDuration(totalDuration)
                .build();
    }

    @Override
    public boolean equals(final Object other) {
        if (this == other) {
            return true;
        }
        if (!(other instanceof ConnectorRetryPolicy)) {
            return false;
        }
        final ConnectorRetryPolicy that = (ConnectorRetryPolicy) other;
        return Double.compare(backoffCoefficient, that.backoffCoefficient) == 0
                && maximumAttempts == that.maximumAttempts
                && initialInterval.equals(that.initialInterval)
                && maximumInterval.equals(that.maximumInterval)
                && totalDuration.equals(that.totalDuration);
    }

    @Override
    public int hashCode() {
        return Objects.hash(initialInterval, backoffCoefficient, maximumInterval, maximumAttempts, totalDuration);
    }
}
