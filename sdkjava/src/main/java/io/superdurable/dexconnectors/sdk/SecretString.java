// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.databind.annotation.JsonSerialize;

/**
 * Credential wrapper that reveals its value only at a provider request boundary.
 *
 * <p>{@link #toString()} is redacted, and the class is deliberately not serializable by Jackson
 * or Java serialization, so a secret cannot leak into Flow state, logs, or results.
 */
@JsonSerialize(using = PrivateValueRefusingSerializer.class)
public final class SecretString {
    private final String value;

    /**
     * Wraps a secret.
     *
     * @param value the secret value
     */
    public SecretString(final String value) {
        if (value == null) {
            throw new IllegalArgumentException("secret value is required");
        }
        this.value = value;
    }

    /**
     * Returns the secret for immediate provider request construction.
     *
     * @return the secret value
     */
    public String reveal() {
        return value;
    }

    /**
     * Reports whether the secret is empty.
     *
     * @return {@code true} for an empty secret
     */
    public boolean isEmpty() {
        return value.isEmpty();
    }

    @Override
    public String toString() {
        return "[REDACTED]";
    }

    @Override
    public boolean equals(final Object other) {
        return other instanceof SecretString && value.equals(((SecretString) other).value);
    }

    @Override
    public int hashCode() {
        return value.hashCode();
    }
}
