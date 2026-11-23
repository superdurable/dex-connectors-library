// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.relaysignature;

import java.time.Instant;

/**
 * The signed parts of one relayed request, parsed before the token is needed, or the refusal.
 */
public final class ParsedRelaySignature {
    private final String timestamp;
    private final Instant signedAt;
    private final byte[] signature;
    private final RelaySignatureRefusal refusal;

    private ParsedRelaySignature(
            final String timestamp, final Instant signedAt, final byte[] signature, final RelaySignatureRefusal refusal) {
        this.timestamp = timestamp;
        this.signedAt = signedAt;
        this.signature = signature;
        this.refusal = refusal;
    }

    static ParsedRelaySignature parsed(final String timestamp, final Instant signedAt, final byte[] signature) {
        return new ParsedRelaySignature(timestamp, signedAt, signature, null);
    }

    static ParsedRelaySignature refused(final RelaySignatureRefusal refusal) {
        return new ParsedRelaySignature("", null, new byte[0], refusal);
    }

    /**
     * Returns why the headers are refused.
     *
     * @return the refusal, or {@code null} when the headers parsed within the window
     */
    public RelaySignatureRefusal getRefusal() {
        return refusal;
    }

    /**
     * Returns the timestamp header text, used verbatim in the signed message.
     *
     * @return the timestamp, or empty for a refusal
     */
    public String getTimestamp() {
        return timestamp;
    }

    /**
     * Returns when the host signed the request.
     *
     * @return the signing second, or {@code null} for a refusal
     */
    public Instant getSignedAt() {
        return signedAt;
    }

    byte[] signature() {
        return signature;
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return refusal == null ? "ParsedRelaySignature{signedAt=" + signedAt + "}"
                : "ParsedRelaySignature{refusal=" + refusal.getWireValue() + "}";
    }
}
