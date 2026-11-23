// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Objects;

/**
 * Either one ordinary application environment value or one private pinned secret reference.
 */
public final class EnvironmentValue {
    private final String value;
    private final ApplicationSecretRef secretRef;

    private EnvironmentValue(final String value, final ApplicationSecretRef secretRef) {
        this.value = value;
        this.secretRef = secretRef;
    }

    /**
     * Creates an ordinary value.
     *
     * @param value the value
     * @return the selection
     */
    public static EnvironmentValue ofValue(final String value) {
        if (value == null) {
            throw new IllegalArgumentException("environment value is required");
        }
        return new EnvironmentValue(value, null);
    }

    /**
     * Creates a pinned secret selection.
     *
     * @param secretRef the secret reference
     * @return the selection
     */
    public static EnvironmentValue ofSecret(final ApplicationSecretRef secretRef) {
        if (secretRef == null) {
            throw new IllegalArgumentException("environment secret reference is required");
        }
        return new EnvironmentValue(null, secretRef);
    }

    /**
     * Returns the ordinary value.
     *
     * @return the value, or {@code null} for a secret selection
     */
    public String getValue() {
        return value;
    }

    /**
     * Returns the secret reference.
     *
     * @return the reference, or {@code null} for an ordinary value
     */
    public ApplicationSecretRef getSecretRef() {
        return secretRef;
    }

    /**
     * Encodes the mutually exclusive selection.
     *
     * @return the JSON object
     */
    public ObjectNode toJson() {
        final ObjectNode result = GoCompatibleJson.newObject();
        if (value != null) {
            result.put("value", value);
        } else {
            result.set("secretRef", secretRef.toJson());
        }
        return result;
    }

    /**
     * Decodes one environment selection without exposing secret contents.
     *
     * @param value the JSON value
     * @return the selection
     * @throws IllegalArgumentException if the selection is invalid
     */
    public static EnvironmentValue fromJson(final JsonNode value) {
        final ObjectNode record = GoCompatibleJson.requireObject(value, "environment value");
        if (record.size() == 1 && record.has("value") && record.get("value").isTextual()) {
            return ofValue(record.get("value").textValue());
        }
        if (record.size() == 1 && record.has("secretRef")) {
            return ofSecret(ApplicationSecretRef.fromJson(record.get("secretRef")));
        }
        throw new IllegalArgumentException("invalid environment selection");
    }

    /** {@inheritDoc} */
    @Override
    public boolean equals(final Object other) {
        if (!(other instanceof EnvironmentValue)) {
            return false;
        }
        final EnvironmentValue that = (EnvironmentValue) other;
        return Objects.equals(value, that.value) && Objects.equals(secretRef, that.secretRef);
    }

    /** {@inheritDoc} */
    @Override
    public int hashCode() {
        return Objects.hash(value, secretRef);
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return secretRef != null ? "EnvironmentValue{secretRef}" : "EnvironmentValue{value}";
    }
}
