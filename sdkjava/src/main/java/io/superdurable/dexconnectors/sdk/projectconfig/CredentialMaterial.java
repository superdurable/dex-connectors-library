// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.annotation.JsonSerialize;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.PrivateValueRefusingSerializer;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.time.Instant;

/**
 * Private connector credential material; never put it in Flow state, logs, or results.
 *
 * <p>{@link #toString()} is redacted, the credentials are copied on access, and Jackson refuses
 * to serialize it, as Go's {@code MarshalJSON} does.
 */
@JsonSerialize(using = PrivateValueRefusingSerializer.class)
public final class CredentialMaterial {
    private final ObjectNode credentials;
    private final Rfc3339NanoTimestamp expiresAt;
    private final String authMethod;

    /**
     * Creates credential material.
     *
     * @param credentials the private credential object
     * @param expiresAt the expiry, or {@code null} when it does not expire
     * @param authMethod the authorization method ID
     */
    public CredentialMaterial(
            final ObjectNode credentials, final Rfc3339NanoTimestamp expiresAt, final String authMethod) {
        if (credentials == null) {
            throw new IllegalArgumentException("credential material is required");
        }
        this.credentials = credentials.deepCopy();
        this.expiresAt = expiresAt;
        this.authMethod = authMethod == null ? "" : authMethod;
    }

    /**
     * Returns a copy of the private credential object.
     *
     * @return the credentials
     */
    public ObjectNode getCredentials() {
        return credentials.deepCopy();
    }

    /**
     * Returns the expiry.
     *
     * @return the expiry, or {@code null} when it does not expire
     */
    public Rfc3339NanoTimestamp getExpiresAt() {
        return expiresAt;
    }

    /**
     * Returns the expiry as an instant.
     *
     * @return the expiry instant, or {@code null}
     */
    public Instant getExpiresAtInstant() {
        return expiresAt == null ? null : expiresAt.toOffsetDateTime().toInstant();
    }

    /**
     * Returns the authorization method ID.
     *
     * @return the method ID
     */
    public String getAuthMethod() {
        return authMethod;
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return "CredentialMaterial([REDACTED])";
    }
}
