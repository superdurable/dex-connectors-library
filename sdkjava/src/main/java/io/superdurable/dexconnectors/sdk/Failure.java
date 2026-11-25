// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;
import java.util.Objects;

/**
 * Secret-safe provider or local failure that may be persisted in a Flow result.
 *
 * <p>Instances are immutable. The constructor validates every field, so a constructed failure is
 * always encodable. Never put credentials, tokens, or personal data in the message.
 */
@JsonPropertyOrder({"kind", "provider", "operation", "message"})
@JsonIgnoreProperties(ignoreUnknown = false)
public final class Failure {
    private final FailureKind kind;
    private final String provider;
    private final String operation;
    private final String message;

    /**
     * Creates a validated failure.
     *
     * @param kind the provider or local fact
     * @param provider the connector ID that observed the failure
     * @param operation the operation ID that observed the failure
     * @param message the secret-safe explanation
     * @throws IllegalArgumentException if any field is null or empty
     */
    @JsonCreator
    public Failure(
            @JsonProperty(value = "kind", required = true) final FailureKind kind,
            @JsonProperty(value = "provider", required = true) final String provider,
            @JsonProperty(value = "operation", required = true) final String operation,
            @JsonProperty(value = "message", required = true) final String message) {
        if (kind == null) {
            throw new IllegalArgumentException("failure kind is invalid");
        }
        if (isEmpty(provider) || isEmpty(operation) || isEmpty(message)) {
            throw new IllegalArgumentException("failure provider, operation, and message are required");
        }
        this.kind = kind;
        this.provider = provider;
        this.operation = operation;
        this.message = message;
    }

    /**
     * Returns the provider or local fact.
     *
     * @return the failure kind
     */
    @JsonProperty("kind")
    public FailureKind getKind() {
        return kind;
    }

    /**
     * Returns the connector ID that observed the failure.
     *
     * @return the provider
     */
    @JsonProperty("provider")
    public String getProvider() {
        return provider;
    }

    /**
     * Returns the operation ID that observed the failure.
     *
     * @return the operation
     */
    @JsonProperty("operation")
    public String getOperation() {
        return operation;
    }

    /**
     * Returns the secret-safe explanation.
     *
     * @return the message
     */
    @JsonProperty("message")
    public String getMessage() {
        return message;
    }

    @Override
    public boolean equals(final Object other) {
        if (this == other) {
            return true;
        }
        if (!(other instanceof Failure)) {
            return false;
        }
        final Failure that = (Failure) other;
        return kind == that.kind
                && provider.equals(that.provider)
                && operation.equals(that.operation)
                && message.equals(that.message);
    }

    @Override
    public int hashCode() {
        return Objects.hash(kind, provider, operation, message);
    }

    @Override
    public String toString() {
        return "Failure{" + kind + " " + provider + " " + operation + ": " + message + "}";
    }

    private static boolean isEmpty(final String value) {
        return value == null || value.isEmpty();
    }
}
