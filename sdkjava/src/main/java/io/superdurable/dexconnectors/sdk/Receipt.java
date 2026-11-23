// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonAutoDetect.Visibility;
import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;

/**
 * Provider correlation data for one call; zero values are completed by the operation runner.
 *
 * <p>Callers must exclude secrets before construction. The all-zero receipt from
 * {@link #empty()} matches Go's {@code sdkgo.Receipt{}} for a local or uncorrelated outcome;
 * {@link ConnectorOperations} fills in the Call ID, provider, idempotency key, and observation
 * time.
 */
@JsonPropertyOrder({"callId", "idempotencyKey", "provider", "providerObjectId", "providerRequestId",
        "observedAt", "metadata"})
@JsonAutoDetect(getterVisibility = Visibility.NONE, isGetterVisibility = Visibility.NONE,
        fieldVisibility = Visibility.NONE)
public final class Receipt {
    private static final Receipt EMPTY = new Receipt(CallId.EMPTY, "", Rfc3339NanoTimestamp.ZERO, "", "", "", null);

    private final CallId callId;
    private final String provider;
    private final Rfc3339NanoTimestamp observedAt;
    private final String idempotencyKey;
    private final String providerObjectId;
    private final String providerRequestId;
    private final Map<String, String> metadata;

    /**
     * Creates a receipt.
     *
     * @param callId the Call ID, or {@link CallId#EMPTY} for the runner to complete
     * @param provider the provider, or empty for the connector ID
     * @param observedAt the observation time, or {@link Rfc3339NanoTimestamp#ZERO} for now
     * @param idempotencyKey the provider idempotency key, or empty
     * @param providerObjectId the provider object ID, or empty
     * @param providerRequestId the provider request ID, or empty
     * @param metadata secret-safe provider metadata, or {@code null}
     */
    public Receipt(
            final CallId callId,
            final String provider,
            final Rfc3339NanoTimestamp observedAt,
            final String idempotencyKey,
            final String providerObjectId,
            final String providerRequestId,
            final Map<String, String> metadata) {
        if (callId == null || provider == null || observedAt == null) {
            throw new IllegalArgumentException("receipt fields are invalid");
        }
        this.callId = callId;
        this.provider = provider;
        this.observedAt = observedAt;
        this.idempotencyKey = idempotencyKey == null ? "" : idempotencyKey;
        this.providerObjectId = providerObjectId == null ? "" : providerObjectId;
        this.providerRequestId = providerRequestId == null ? "" : providerRequestId;
        if (metadata == null || metadata.isEmpty()) {
            this.metadata = Collections.emptyMap();
        } else {
            for (Map.Entry<String, String> entry : metadata.entrySet()) {
                if (entry.getKey() == null || entry.getValue() == null) {
                    throw new IllegalArgumentException("receipt metadata is invalid");
                }
            }
            this.metadata = Collections.unmodifiableMap(new LinkedHashMap<>(metadata));
        }
    }

    @JsonCreator
    static Receipt fromWire(
            @JsonProperty(value = "callId", required = true) final CallId callId,
            @JsonProperty(value = "provider", required = true) final String provider,
            @JsonProperty(value = "observedAt", required = true) final Rfc3339NanoTimestamp observedAt,
            @JsonProperty("idempotencyKey") final String idempotencyKey,
            @JsonProperty("providerObjectId") final String providerObjectId,
            @JsonProperty("providerRequestId") final String providerRequestId,
            @JsonProperty("metadata") final Map<String, String> metadata) {
        if (callId != null && !callId.isEmpty()) {
            callId.validate();
        }
        return new Receipt(callId, provider, observedAt, idempotencyKey, providerObjectId, providerRequestId,
                metadata);
    }

    /**
     * Returns the all-zero receipt for a local or uncorrelated provider outcome.
     *
     * @return the empty receipt
     */
    public static Receipt empty() {
        return EMPTY;
    }

    /**
     * Starts a receipt builder with every field at its zero value.
     *
     * @return a new builder
     */
    public static Builder newBuilder() {
        return new Builder();
    }

    /**
     * Returns a builder initialized from this receipt.
     *
     * @return a new builder
     */
    public Builder toBuilder() {
        final Builder builder = new Builder();
        builder.callId = callId;
        builder.provider = provider;
        builder.observedAt = observedAt;
        builder.idempotencyKey = idempotencyKey;
        builder.providerObjectId = providerObjectId;
        builder.providerRequestId = providerRequestId;
        builder.metadata = new LinkedHashMap<>(metadata);
        return builder;
    }

    /**
     * Returns the Call ID.
     *
     * @return the Call ID, empty before completion
     */
    @JsonProperty("callId")
    public CallId getCallId() {
        return callId;
    }

    /**
     * Returns the provider name.
     *
     * @return the provider, empty before completion
     */
    @JsonProperty("provider")
    public String getProvider() {
        return provider;
    }

    /**
     * Returns the observation time.
     *
     * @return the timestamp
     */
    @JsonProperty("observedAt")
    public Rfc3339NanoTimestamp getObservedAt() {
        return observedAt;
    }

    /**
     * Returns the provider idempotency key.
     *
     * @return the key, or empty
     */
    @JsonProperty("idempotencyKey")
    @JsonInclude(JsonInclude.Include.NON_EMPTY)
    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    /**
     * Returns the provider object ID.
     *
     * @return the object ID, or empty
     */
    @JsonProperty("providerObjectId")
    @JsonInclude(JsonInclude.Include.NON_EMPTY)
    public String getProviderObjectId() {
        return providerObjectId;
    }

    /**
     * Returns the provider request ID.
     *
     * @return the request ID, or empty
     */
    @JsonProperty("providerRequestId")
    @JsonInclude(JsonInclude.Include.NON_EMPTY)
    public String getProviderRequestId() {
        return providerRequestId;
    }

    /**
     * Returns secret-safe provider metadata.
     *
     * @return the unmodifiable metadata, possibly empty
     */
    @JsonProperty("metadata")
    @JsonInclude(JsonInclude.Include.NON_EMPTY)
    public Map<String, String> getMetadata() {
        return metadata;
    }

    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof Receipt)) {
            return false;
        }
        final Receipt that = (Receipt) other;
        return callId.equals(that.callId)
                && provider.equals(that.provider)
                && observedAt.equals(that.observedAt)
                && idempotencyKey.equals(that.idempotencyKey)
                && providerObjectId.equals(that.providerObjectId)
                && providerRequestId.equals(that.providerRequestId)
                && metadata.equals(that.metadata);
    }

    @Override
    public int hashCode() {
        return Objects.hash(callId, provider, observedAt, idempotencyKey, providerObjectId, providerRequestId,
                metadata);
    }

    /**
     * Builds a {@link Receipt}; unset fields keep their zero values.
     */
    public static final class Builder {
        private CallId callId = CallId.EMPTY;
        private String provider = "";
        private Rfc3339NanoTimestamp observedAt = Rfc3339NanoTimestamp.ZERO;
        private String idempotencyKey = "";
        private String providerObjectId = "";
        private String providerRequestId = "";
        private Map<String, String> metadata = new LinkedHashMap<>();

        private Builder() {
        }

        /**
         * Sets the Call ID.
         *
         * @param value the Call ID
         * @return this builder
         */
        public Builder callId(final CallId value) {
            this.callId = value;
            return this;
        }

        /**
         * Sets the provider name.
         *
         * @param value the provider
         * @return this builder
         */
        public Builder provider(final String value) {
            this.provider = value;
            return this;
        }

        /**
         * Sets the observation time.
         *
         * @param value the timestamp
         * @return this builder
         */
        public Builder observedAt(final Rfc3339NanoTimestamp value) {
            this.observedAt = value;
            return this;
        }

        /**
         * Sets the provider idempotency key.
         *
         * @param value the key
         * @return this builder
         */
        public Builder idempotencyKey(final String value) {
            this.idempotencyKey = value;
            return this;
        }

        /**
         * Sets the provider object ID.
         *
         * @param value the object ID
         * @return this builder
         */
        public Builder providerObjectId(final String value) {
            this.providerObjectId = value;
            return this;
        }

        /**
         * Sets the provider request ID.
         *
         * @param value the request ID
         * @return this builder
         */
        public Builder providerRequestId(final String value) {
            this.providerRequestId = value;
            return this;
        }

        /**
         * Adds one secret-safe metadata entry.
         *
         * @param key the metadata key
         * @param value the metadata value
         * @return this builder
         */
        public Builder putMetadata(final String key, final String value) {
            this.metadata.put(key, value);
            return this;
        }

        /**
         * Builds the receipt.
         *
         * @return the receipt
         */
        public Receipt build() {
            return new Receipt(callId, provider, observedAt, idempotencyKey, providerObjectId, providerRequestId,
                    metadata);
        }
    }
}
