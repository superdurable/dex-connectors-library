// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.time.Instant;

/**
 * Safe authoritative connection metadata without credential bytes.
 */
public final class ProjectConnection {
    private final ConnectionKey key;
    private final Scope scope;
    private final long revision;
    private final long fence;
    private final String status;
    private final Instant expiresAt;
    private final String authMethod;

    ProjectConnection(
            final ConnectionKey key, final Scope scope, final long revision, final long fence, final String status,
            final Instant expiresAt, final String authMethod) {
        this.key = key;
        this.scope = scope;
        this.revision = revision;
        this.fence = fence;
        this.status = status;
        this.expiresAt = expiresAt;
        this.authMethod = authMethod;
    }

    /**
     * Returns the connection key.
     *
     * @return the key
     */
    public ConnectionKey getKey() {
        return key;
    }

    /**
     * Returns the project scope.
     *
     * @return the scope
     */
    public Scope getScope() {
        return scope;
    }

    /**
     * Returns the head revision.
     *
     * @return the revision
     */
    public long getRevision() {
        return revision;
    }

    /**
     * Returns the credential fence that orders credential generations.
     *
     * @return the fence
     */
    public long getFence() {
        return fence;
    }

    /**
     * Returns the status: {@code READY}, {@code REFRESHING}, {@code AUTHORIZING},
     * {@code REAUTHORIZATION_REQUIRED}, or {@code REVOKED}.
     *
     * @return the status
     */
    public String getStatus() {
        return status;
    }

    /**
     * Returns the credential expiry.
     *
     * @return the expiry, or {@code null}
     */
    public Instant getExpiresAt() {
        return expiresAt;
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
        return "ProjectConnection{" + key + ", revision=" + revision + ", status=" + status + "}";
    }
}
