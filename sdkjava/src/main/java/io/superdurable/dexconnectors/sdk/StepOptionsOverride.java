// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import io.superdurable.dex.AttributeLock;
import io.superdurable.dex.StepDurability;
import io.superdurable.dex.StepOptions;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * Application override of a Connector Step's Execute options.
 *
 * <p>Connector Steps run only Execute, so the override has no WaitFor options. Unset values keep
 * the operation's manifest defaults. An explicit {@link Builder#executeRetry} replaces the
 * operation policy, including any attempts a raised send cap would add.
 */
public final class StepOptionsOverride {
    private final Duration executeMethodTimeout;
    private final Duration heartbeatTimeout;
    private final ConnectorRetryPolicy executeRetry;
    private final StepDurability executeDurability;
    private final List<AttributeLock> executeLocks;

    private StepOptionsOverride(final Builder builder) {
        this.executeMethodTimeout = builder.executeMethodTimeout;
        this.heartbeatTimeout = builder.heartbeatTimeout;
        this.executeRetry = builder.executeRetry;
        this.executeDurability = builder.executeDurability;
        this.executeLocks = Collections.unmodifiableList(new ArrayList<>(builder.executeLocks));
    }

    /**
     * Starts an override with every value unset.
     *
     * @return a new builder
     */
    public static Builder newBuilder() {
        return new Builder();
    }

    /**
     * Returns the overridden Execute timeout.
     *
     * @return the timeout, or {@code null} for the operation default
     */
    public Duration getExecuteMethodTimeout() {
        return executeMethodTimeout;
    }

    /**
     * Returns the overridden heartbeat timeout.
     *
     * @return the timeout, or {@code null} for the operation default
     */
    public Duration getHeartbeatTimeout() {
        return heartbeatTimeout;
    }

    /**
     * Returns the overridden Execute retry policy.
     *
     * @return the policy, or {@code null} for the operation default
     */
    public ConnectorRetryPolicy getExecuteRetry() {
        return executeRetry;
    }

    /**
     * Returns the overridden Execute durability.
     *
     * @return the durability, or {@code null} for the operation default
     */
    public StepDurability getExecuteDurability() {
        return executeDurability;
    }

    /**
     * Returns Attribute locks held during Execute.
     *
     * @return the unmodifiable locks
     */
    public List<AttributeLock> getExecuteLocks() {
        return executeLocks;
    }

    /**
     * Builds a {@link StepOptionsOverride}.
     */
    public static final class Builder {
        private Duration executeMethodTimeout;
        private Duration heartbeatTimeout;
        private ConnectorRetryPolicy executeRetry;
        private StepDurability executeDurability;
        private final List<AttributeLock> executeLocks = new ArrayList<>();

        private Builder() {
        }

        /**
         * Overrides the Execute timeout.
         *
         * @param value the positive timeout
         * @return this builder
         */
        public Builder executeMethodTimeout(final Duration value) {
            this.executeMethodTimeout = value;
            return this;
        }

        /**
         * Overrides the heartbeat timeout.
         *
         * @param value the positive timeout
         * @return this builder
         */
        public Builder heartbeatTimeout(final Duration value) {
            this.heartbeatTimeout = value;
            return this;
        }

        /**
         * Overrides the Execute retry policy.
         *
         * @param value the retry policy
         * @return this builder
         */
        public Builder executeRetry(final ConnectorRetryPolicy value) {
            this.executeRetry = value;
            return this;
        }

        /**
         * Overrides the Execute durability.
         *
         * @param value the durability; {@link StepDurability#DEFAULT} keeps the operation default
         * @return this builder
         */
        public Builder executeDurability(final StepDurability value) {
            this.executeDurability = value;
            return this;
        }

        /**
         * Adds an Attribute lock held during Execute.
         *
         * @param value the lock
         * @return this builder
         */
        public Builder addExecuteLock(final AttributeLock value) {
            this.executeLocks.add(value);
            return this;
        }

        /**
         * Builds the override.
         *
         * @return the override
         */
        public StepOptionsOverride build() {
            return new StepOptionsOverride(this);
        }
    }

    static StepOptions mergeStepOptions(
            final StepDefaults defaults, final StepOptionsOverride override, final int additionalExecuteAttempts) {
        final ConnectorRetryPolicy policy = defaults.getExecuteRetry() == null
                ? ConnectorRetryPolicy.defaultConnectorStepExecuteRetryPolicy()
                : defaults.getExecuteRetry();
        final StepOptions.Builder builder = StepOptions.newBuilder()
                .executeRetry(policy.toDexRetryPolicy(additionalExecuteAttempts))
                .executeDurability(defaults.toDexStepDurability());
        if (!defaults.getExecuteMethodTimeout().isZero()) {
            builder.executeMethodTimeout(defaults.getExecuteMethodTimeout());
        }
        if (!defaults.getHeartbeatTimeout().isZero()) {
            builder.heartbeatTimeout(defaults.getHeartbeatTimeout());
        }
        if (override == null) {
            return builder.build();
        }
        if (override.executeMethodTimeout != null) {
            builder.executeMethodTimeout(override.executeMethodTimeout);
        }
        if (override.heartbeatTimeout != null) {
            builder.heartbeatTimeout(override.heartbeatTimeout);
        }
        if (override.executeRetry != null) {
            builder.executeRetry(override.executeRetry.toDexRetryPolicy(0));
        }
        if (override.executeDurability != null && override.executeDurability != StepDurability.DEFAULT) {
            builder.executeDurability(override.executeDurability);
        }
        for (AttributeLock lock : override.executeLocks) {
            builder.addExecuteLock(lock);
        }
        return builder.build();
    }
}
