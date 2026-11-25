// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import io.superdurable.dex.StepDurability;
import java.time.Duration;
import java.util.Objects;

/**
 * Execute-only Dex Step defaults owned by an operation definition.
 *
 * <p>The JSON form uses Go nanosecond durations and the Go durability ordinal
 * ({@code 0} default, {@code 1} sync, {@code 2} async). A zero heartbeat timeout means none.
 */
@JsonPropertyOrder({"executeMethodTimeout", "heartbeatTimeout", "executeRetry", "executeDurability"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class StepDefaults {
    private final Duration executeMethodTimeout;
    private final int executeDurability;
    private final Duration heartbeatTimeout;
    private final ConnectorRetryPolicy executeRetry;

    /**
     * Creates validated Step defaults.
     *
     * @param executeMethodTimeout the Execute method timeout
     * @param executeDurability the Go durability ordinal: 0 default, 1 sync, 2 async
     * @param heartbeatTimeout the heartbeat timeout, or zero for none
     * @param executeRetry the Execute retry policy, or {@code null} for the connector default
     * @throws IllegalArgumentException if a timeout is negative or the durability is unknown
     */
    public StepDefaults(
            final Duration executeMethodTimeout,
            final int executeDurability,
            final Duration heartbeatTimeout,
            final ConnectorRetryPolicy executeRetry) {
        if (executeMethodTimeout == null || heartbeatTimeout == null
                || executeMethodTimeout.isNegative() || heartbeatTimeout.isNegative()) {
            throw new IllegalArgumentException("Step timeouts cannot be negative");
        }
        if (executeDurability < 0 || executeDurability > 2) {
            throw new IllegalArgumentException("Step Execute durability is invalid");
        }
        this.executeMethodTimeout = executeMethodTimeout;
        this.executeDurability = executeDurability;
        this.heartbeatTimeout = heartbeatTimeout;
        this.executeRetry = executeRetry;
    }

    /**
     * Creates Step defaults without a heartbeat timeout or explicit retry policy.
     *
     * @param executeMethodTimeout the Execute method timeout
     * @param executeDurability the Go durability ordinal: 0 default, 1 sync, 2 async
     */
    public StepDefaults(final Duration executeMethodTimeout, final int executeDurability) {
        this(executeMethodTimeout, executeDurability, Duration.ZERO, null);
    }

    @JsonCreator
    static StepDefaults fromWire(
            @JsonProperty(value = "executeMethodTimeout", required = true) final long executeMethodTimeoutNanos,
            @JsonProperty(value = "executeDurability", required = true) final int executeDurability,
            @JsonProperty("heartbeatTimeout") final long heartbeatTimeoutNanos,
            @JsonProperty("executeRetry") final ConnectorRetryPolicy executeRetry) {
        return new StepDefaults(Duration.ofNanos(executeMethodTimeoutNanos), executeDurability,
                Duration.ofNanos(heartbeatTimeoutNanos), executeRetry);
    }

    /**
     * Returns the Execute method timeout.
     *
     * @return the timeout
     */
    public Duration getExecuteMethodTimeout() {
        return executeMethodTimeout;
    }

    /**
     * Returns the Go durability ordinal: 0 default, 1 sync, 2 async.
     *
     * @return the durability ordinal
     */
    public int getExecuteDurability() {
        return executeDurability;
    }

    /**
     * Returns the Dex Java SDK durability for {@link #getExecuteDurability()}.
     *
     * @return the Step durability
     */
    public StepDurability toDexStepDurability() {
        switch (executeDurability) {
            case 1:
                return StepDurability.SYNC;
            case 2:
                return StepDurability.ASYNC;
            default:
                return StepDurability.DEFAULT;
        }
    }

    /**
     * Returns the heartbeat timeout, where zero means none.
     *
     * @return the heartbeat timeout
     */
    public Duration getHeartbeatTimeout() {
        return heartbeatTimeout;
    }

    /**
     * Returns the Execute retry policy.
     *
     * @return the policy, or {@code null} for the connector default
     */
    public ConnectorRetryPolicy getExecuteRetry() {
        return executeRetry;
    }

    @JsonProperty("executeMethodTimeout")
    long executeMethodTimeoutNanos() {
        return executeMethodTimeout.toNanos();
    }

    @JsonProperty("heartbeatTimeout")
    @JsonInclude(JsonInclude.Include.NON_DEFAULT)
    long heartbeatTimeoutNanos() {
        return heartbeatTimeout.toNanos();
    }

    @JsonProperty("executeRetry")
    @JsonInclude(JsonInclude.Include.NON_NULL)
    ConnectorRetryPolicy executeRetryWire() {
        return executeRetry;
    }

    @JsonProperty("executeDurability")
    int executeDurabilityWire() {
        return executeDurability;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof StepDefaults)) {
            return false;
        }
        final StepDefaults that = (StepDefaults) other;
        return executeDurability == that.executeDurability
                && executeMethodTimeout.equals(that.executeMethodTimeout)
                && heartbeatTimeout.equals(that.heartbeatTimeout)
                && Objects.equals(executeRetry, that.executeRetry);
    }

    @Override
    public int hashCode() {
        return Objects.hash(executeMethodTimeout, executeDurability, heartbeatTimeout, executeRetry);
    }
}
