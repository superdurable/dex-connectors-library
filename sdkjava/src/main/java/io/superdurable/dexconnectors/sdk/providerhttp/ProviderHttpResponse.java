// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStream;

/**
 * A provider response whose body is still streaming.
 *
 * <p>The caller owns the response and must read it with {@link #readBoundedBody(long)}, which
 * closes it on every path, or close it explicitly.
 */
public final class ProviderHttpResponse implements Closeable {
    private final int statusCode;
    private final ProviderHttpHeaders headers;
    private final InputStream body;
    private final Closeable release;

    /**
     * Creates a response.
     *
     * @param statusCode the HTTP status
     * @param headers the response headers
     * @param body the body stream, or {@code null} for an empty body
     * @param release releases the underlying connection, or {@code null}
     */
    public ProviderHttpResponse(
            final int statusCode, final ProviderHttpHeaders headers, final InputStream body, final Closeable release) {
        this.statusCode = statusCode;
        this.headers = headers == null ? ProviderHttpHeaders.EMPTY : headers;
        this.body = body == null ? new ByteArrayInputStream(new byte[0]) : body;
        this.release = release;
    }

    /**
     * Creates a fully buffered response, such as from a test transport.
     *
     * @param statusCode the HTTP status
     * @param headers the response headers
     * @param body the body bytes
     * @return the response
     */
    public static ProviderHttpResponse buffered(
            final int statusCode, final ProviderHttpHeaders headers, final byte[] body) {
        return new ProviderHttpResponse(statusCode, headers, new ByteArrayInputStream(body.clone()), null);
    }

    /**
     * Returns the HTTP status.
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

    /**
     * Reads at most {@code maximumBytes} and closes the response on every path.
     *
     * @param maximumBytes the body limit; a negative value means zero
     * @return the body bytes
     * @throws BodyTooLargeException if the body exceeds the limit
     * @throws ProviderTransportException if the body read fails; it is possibly sent
     */
    public byte[] readBoundedBody(final long maximumBytes) throws ProviderTransportException {
        final long limit = Math.max(0, maximumBytes);
        final ByteArrayOutputStream contents = new ByteArrayOutputStream();
        final byte[] buffer = new byte[8192];
        try {
            long count = 0;
            while (true) {
                final int read;
                try {
                    read = body.read(buffer);
                } catch (IOException error) {
                    throw new ProviderTransportException(false, statusCode, headers);
                }
                if (read < 0) {
                    break;
                }
                count += read;
                if (count > limit) {
                    throw new BodyTooLargeException(statusCode, headers);
                }
                contents.write(buffer, 0, read);
            }
        } finally {
            close();
        }
        return contents.toByteArray();
    }

    /**
     * Closes the body and releases the connection.
     */
    @Override
    public void close() {
        try {
            body.close();
        } catch (IOException ignored) {
            // Closing a provider stream is best effort; the connection is released below.
        }
        if (release != null) {
            try {
                release.close();
            } catch (IOException ignored) {
                // Releasing the connection is best effort.
            }
        }
    }
}
