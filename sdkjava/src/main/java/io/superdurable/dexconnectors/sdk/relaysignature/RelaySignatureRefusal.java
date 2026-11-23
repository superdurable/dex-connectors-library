// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.relaysignature;

/**
 * Why a relayed request is refused.
 */
public enum RelaySignatureRefusal {
    /** A relay header is absent or empty. */
    MISSING("missing"),
    /** A relay header does not have the v1 syntax. */
    MALFORMED("malformed"),
    /** The timestamp is more than five minutes from receipt. */
    STALE("stale"),
    /** The signature does not match the token over the raw body. */
    MISMATCH("mismatch");

    private final String wireValue;

    RelaySignatureRefusal(final String wireValue) {
        this.wireValue = wireValue;
    }

    /**
     * Returns the value shared with the Go, Python, and TypeScript SDKs.
     *
     * @return the lowercase refusal name
     */
    public String getWireValue() {
        return wireValue;
    }
}
