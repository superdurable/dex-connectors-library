// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Objects;

/**
 * Credential broker endpoint and the exact scope-token reference for shared credentials.
 */
public final class CredentialBrokerConfiguration {
    private final String endpoint;
    private final ApplicationSecretRef tokenSecretRef;

    /**
     * Creates a broker configuration.
     *
     * @param endpoint the absolute broker read endpoint
     * @param tokenSecretRef the pinned scope-token secret
     */
    public CredentialBrokerConfiguration(final String endpoint, final ApplicationSecretRef tokenSecretRef) {
        if (tokenSecretRef == null) {
            throw new IllegalArgumentException("credential broker token reference is required");
        }
        this.endpoint = endpoint == null ? "" : endpoint;
        this.tokenSecretRef = tokenSecretRef;
    }

    /**
     * Returns the broker read endpoint.
     *
     * @return the endpoint URL
     */
    public String getEndpoint() {
        return endpoint;
    }

    /**
     * Returns the pinned scope-token secret.
     *
     * @return the secret reference
     */
    public ApplicationSecretRef getTokenSecretRef() {
        return tokenSecretRef;
    }

    /**
     * Encodes the broker configuration.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        final ObjectNode result = GoCompatibleJson.newObject();
        result.put("endpoint", endpoint);
        result.set("tokenSecretRef", tokenSecretRef.toJson());
        return result;
    }

    /**
     * Decodes one broker configuration.
     *
     * @param value the JSON value
     * @return the configuration
     * @throws IllegalArgumentException if the value is invalid
     */
    public static CredentialBrokerConfiguration fromJson(final JsonNode value) {
        final ObjectNode record = GoCompatibleJson.requireObject(value, "credential broker");
        ProjectConfigurationRules.requireFields(record, "credential broker", "endpoint", "tokenSecretRef");
        return new CredentialBrokerConfiguration(ProjectConfigurationRules.text(record, "endpoint"),
                ApplicationSecretRef.fromJson(record.get("tokenSecretRef")));
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof CredentialBrokerConfiguration)) {
            return false;
        }
        final CredentialBrokerConfiguration that = (CredentialBrokerConfiguration) other;
        return endpoint.equals(that.endpoint) && tokenSecretRef.equals(that.tokenSecretRef);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(endpoint, tokenSecretRef);
    }
}
