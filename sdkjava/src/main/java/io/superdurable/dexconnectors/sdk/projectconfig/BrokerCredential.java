// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.node.ObjectNode;
import java.time.Instant;
import java.time.OffsetDateTime;

/**
 * Private shared credential body and its broker revision metadata.
 *
 * <p>{@link #toString()} is redacted and the credential is copied on access.
 */
public final class BrokerCredential {
    private final ObjectNode credential;
    private final long revision;
    private final OffsetDateTime issuedAt;
    private final OffsetDateTime expiresAt;
    private final Instant readAt;

    BrokerCredential(
            final ObjectNode credential, final long revision, final OffsetDateTime issuedAt,
            final OffsetDateTime expiresAt, final Instant readAt) {
        this.credential = credential.deepCopy();
        this.revision = revision;
        this.issuedAt = issuedAt;
        this.expiresAt = expiresAt;
        this.readAt = readAt;
    }

    /**
     * Returns a copy of the private credential body.
     *
     * @return the credential
     */
    public ObjectNode getCredential() {
        return credential.deepCopy();
    }

    /**
     * Returns the broker revision.
     *
     * @return the positive revision
     */
    public long getRevision() {
        return revision;
    }

    /**
     * Returns when the broker issued the credential, with its original offset.
     *
     * @return the issue time, or {@code null}
     */
    public OffsetDateTime getIssuedAt() {
        return issuedAt;
    }

    /**
     * Returns when the credential expires, with its original offset.
     *
     * @return the expiry, or {@code null}
     */
    public OffsetDateTime getExpiresAt() {
        return expiresAt;
    }

    /**
     * Returns when this process read the credential.
     *
     * @return the read time
     */
    public Instant getReadAt() {
        return readAt;
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return "BrokerCredential(revision=" + revision + ", credential=[REDACTED])";
    }
}
