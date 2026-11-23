// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

/**
 * A provider body exceeded its limit; status and headers remain available for classification.
 *
 * <p>The message omits header values.
 */
public final class BodyTooLargeException extends IllegalArgumentException {
    private static final long serialVersionUID = 1L;

    private final int statusCode;
    private final transient ProviderHttpHeaders headers;

    /**
     * Creates the exception.
     *
     * @param statusCode the response status
     * @param headers the response headers
     */
    public BodyTooLargeException(final int statusCode, final ProviderHttpHeaders headers) {
        super("provider response body exceeds its size limit");
        this.statusCode = statusCode;
        this.headers = headers;
    }

    /**
     * Returns the response status.
     *
     * @return the status code
     */
    public int getStatusCode() {
        return statusCode;
    }

    /**
     * Returns the response headers.
     *
     * @return the headers
     */
    public ProviderHttpHeaders getHeaders() {
        return headers;
    }
}
