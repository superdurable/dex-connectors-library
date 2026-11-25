// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.io.IOException;

/**
 * Secret-safe transport failure with dispatch evidence and any response metadata.
 *
 * <p>{@link #isProvablyNotSent()} is {@code true} only for a DNS, TCP connect, or TLS handshake
 * failure before the request was written, so the provider cannot have processed it. Any later
 * failure is possibly sent. The message never contains the URL, headers, or body.
 */
public final class ProviderTransportException extends IOException {
    private static final long serialVersionUID = 1L;

    private final boolean isProvablyNotSent;
    private final Integer statusCode;
    private final transient ProviderHttpHeaders headers;

    /**
     * Creates a transport failure.
     *
     * @param isProvablyNotSent whether the request provably never left the process
     * @param statusCode the response status when headers arrived, or {@code null}
     * @param headers the response headers when they arrived, or {@code null}
     */
    public ProviderTransportException(
            final boolean isProvablyNotSent, final Integer statusCode, final ProviderHttpHeaders headers) {
        super("provider request failed");
        this.isProvablyNotSent = isProvablyNotSent;
        this.statusCode = statusCode;
        this.headers = headers;
    }

    /**
     * Reports whether the request provably never reached the provider.
     *
     * @return {@code true} for a failure before the request was written
     */
    public boolean isProvablyNotSent() {
        return isProvablyNotSent;
    }

    /**
     * Returns the response status when headers arrived before the failure.
     *
     * @return the status, or {@code null}
     */
    public Integer getStatusCode() {
        return statusCode;
    }

    /**
     * Returns the response headers when they arrived before the failure.
     *
     * @return the headers, or {@code null}
     */
    public ProviderHttpHeaders getHeaders() {
        return headers;
    }
}
