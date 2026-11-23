// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.CredentialProvider;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.trigger.TriggerLogger;
import java.time.Clock;
import java.time.Duration;

/**
 * Static configuration of one connection's webhook endpoint for one Trigger.
 *
 * @param <C> the credential type
 * @param <P> the payload type
 */
public final class WebhookEndpointConfig<C, P> {
    /** Events one binding's source queues before the endpoint answers 503. */
    public static final int DEFAULT_SOURCE_QUEUE_CAPACITY = 64;
    /** How long a verified request may wait to be recorded, measured from when it reserves its slot. */
    public static final Duration DEFAULT_ARRIVAL_TIMEOUT = Duration.ofSeconds(10);

    private final String connectorId;
    private final String triggerName;
    private final ConnectionRef connection;
    private final CredentialProvider<C> credentials;
    private final WebhookRequestVerifier<C> verifyRequest;
    private final WebhookEventDecoder<P> decodeEvent;
    private final int maximumBodyBytes;
    private final CredentialRefreshDriver<C> credentialRefresh;
    private final Clock clock;
    private final int sourceQueueCapacity;
    private final Duration arrivalTimeout;
    private final TriggerLogger logger;

    private WebhookEndpointConfig(final Builder<C, P> builder) {
        this.connectorId = builder.connectorId;
        this.triggerName = builder.triggerName;
        this.connection = builder.connection;
        this.credentials = builder.credentials;
        this.verifyRequest = builder.verifyRequest;
        this.decodeEvent = builder.decodeEvent;
        this.maximumBodyBytes = builder.maximumBodyBytes;
        this.credentialRefresh = builder.credentialRefresh;
        this.clock = builder.clock;
        this.sourceQueueCapacity = builder.sourceQueueCapacity;
        this.arrivalTimeout = builder.arrivalTimeout;
        this.logger = builder.logger;
    }

    /**
     * Starts a builder with the required values.
     *
     * @param connectorId the manifest connector ID
     * @param triggerName the manifest Trigger name
     * @param connection the connection whose credentials verify requests
     * @param credentials resolves the connection's credentials for each request
     * @param verifyRequest verifies a request
     * @param decodeEvent decodes a verified request
     * @param maximumBodyBytes the largest accepted body in bytes; positive
     * @param <C> the credential type
     * @param <P> the payload type
     * @return a new builder
     */
    public static <C, P> Builder<C, P> newBuilder(
            final String connectorId, final String triggerName, final ConnectionRef connection,
            final CredentialProvider<C> credentials, final WebhookRequestVerifier<C> verifyRequest,
            final WebhookEventDecoder<P> decodeEvent, final int maximumBodyBytes) {
        return new Builder<C, P>(connectorId, triggerName, connection, credentials, verifyRequest, decodeEvent,
                maximumBodyBytes);
    }

    String getConnectorId() {
        return connectorId;
    }

    String getTriggerName() {
        return triggerName;
    }

    ConnectionRef getConnection() {
        return connection;
    }

    CredentialProvider<C> getCredentials() {
        return credentials;
    }

    WebhookRequestVerifier<C> getVerifyRequest() {
        return verifyRequest;
    }

    WebhookEventDecoder<P> getDecodeEvent() {
        return decodeEvent;
    }

    int getMaximumBodyBytes() {
        return maximumBodyBytes;
    }

    CredentialRefreshDriver<C> getCredentialRefresh() {
        return credentialRefresh;
    }

    Clock getClock() {
        return clock;
    }

    int getSourceQueueCapacity() {
        return sourceQueueCapacity;
    }

    Duration getArrivalTimeout() {
        return arrivalTimeout;
    }

    TriggerLogger getLogger() {
        return logger;
    }

    /**
     * Builds a {@link WebhookEndpointConfig}.
     *
     * @param <C> the credential type
     * @param <P> the payload type
     */
    public static final class Builder<C, P> {
        private final String connectorId;
        private final String triggerName;
        private final ConnectionRef connection;
        private final CredentialProvider<C> credentials;
        private final WebhookRequestVerifier<C> verifyRequest;
        private final WebhookEventDecoder<P> decodeEvent;
        private final int maximumBodyBytes;
        private CredentialRefreshDriver<C> credentialRefresh;
        private Clock clock = Clock.systemUTC();
        private int sourceQueueCapacity = DEFAULT_SOURCE_QUEUE_CAPACITY;
        private Duration arrivalTimeout = DEFAULT_ARRIVAL_TIMEOUT;
        private TriggerLogger logger;

        private Builder(
                final String connectorId, final String triggerName, final ConnectionRef connection,
                final CredentialProvider<C> credentials, final WebhookRequestVerifier<C> verifyRequest,
                final WebhookEventDecoder<P> decodeEvent, final int maximumBodyBytes) {
            this.connectorId = connectorId;
            this.triggerName = triggerName;
            this.connection = connection;
            this.credentials = credentials;
            this.verifyRequest = verifyRequest;
            this.decodeEvent = decodeEvent;
            this.maximumBodyBytes = maximumBodyBytes;
        }

        /**
         * Resolves credentials through a refresh driver.
         *
         * @param value the refresh driver
         * @return this builder
         */
        public Builder<C, P> credentialRefresh(final CredentialRefreshDriver<C> value) {
            this.credentialRefresh = value;
            return this;
        }

        /**
         * Sets the clock that stamps receipt time; defaults to the UTC system clock.
         *
         * @param value the clock
         * @return this builder
         */
        public Builder<C, P> clock(final Clock value) {
            this.clock = value;
            return this;
        }

        /**
         * Sets how many events each source queues; at least 1.
         *
         * @param value the queue capacity
         * @return this builder
         */
        public Builder<C, P> sourceQueueCapacity(final int value) {
            this.sourceQueueCapacity = value;
            return this;
        }

        /**
         * Sets how long a verified request may wait to be recorded; positive.
         *
         * @param value the arrival timeout
         * @return this builder
         */
        public Builder<C, P> arrivalTimeout(final Duration value) {
            this.arrivalTimeout = value;
            return this;
        }

        /**
         * Sets the destination of the sources' delivery records.
         *
         * @param value the logger, or {@code null} for the default
         * @return this builder
         */
        public Builder<C, P> logger(final TriggerLogger value) {
            this.logger = value;
            return this;
        }

        /**
         * Builds the configuration.
         *
         * @return the configuration
         */
        public WebhookEndpointConfig<C, P> build() {
            return new WebhookEndpointConfig<C, P>(this);
        }
    }
}
