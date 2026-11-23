// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

/**
 * An address policy refused a request before dialing.
 *
 * <p>The reason never contains the host, URL, or credential. Nothing was sent.
 */
public final class AddressRefusalException extends IllegalArgumentException {
    private static final long serialVersionUID = 1L;

    private final String reason;

    /**
     * Creates a refusal.
     *
     * @param reason the secret-safe reason
     */
    public AddressRefusalException(final String reason) {
        super("address not allowed: " + reason);
        this.reason = reason;
    }

    /**
     * Returns the secret-safe reason.
     *
     * @return the reason
     */
    public String getReason() {
        return reason;
    }
}
