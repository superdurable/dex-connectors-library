// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.relaysignature;

import java.time.Instant;

/**
 * The outcome of verifying one relayed request.
 */
public final class RelaySignatureVerification {
    private final Instant signedAt;
    private final RelaySignatureRefusal refusal;

    RelaySignatureVerification(final Instant signedAt, final RelaySignatureRefusal refusal) {
        this.signedAt = signedAt;
        this.refusal = refusal;
    }

    /**
     * Returns when the host signed a valid request.
     *
     * @return the signing second, or {@code null} for a refusal
     */
    public Instant getSignedAt() {
        return signedAt;
    }

    /**
     * Returns why the request is refused.
     *
     * @return the refusal, or {@code null} for a valid request
     */
    public RelaySignatureRefusal getRefusal() {
        return refusal;
    }

    /**
     * Reports whether the request is validly signed.
     *
     * @return {@code true} without a refusal
     */
    public boolean isValid() {
        return refusal == null;
    }
}
