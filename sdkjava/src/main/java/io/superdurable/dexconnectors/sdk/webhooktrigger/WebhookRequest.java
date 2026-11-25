// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import java.time.Instant;
import java.util.AbstractMap;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * One received webhook request with its complete raw body.
 */
public final class WebhookRequest {
    private final String url;
    private final List<Map.Entry<String, String>> headers;
    private final byte[] body;
    private final Instant receivedAt;

    /**
     * Creates a request; header names are lower-cased.
     *
     * @param url the raw request target: path and query exactly as received
     * @param headers the header pairs in arrival order; duplicates are kept
     * @param body the complete raw body, as signatures cover it
     * @param receivedAt when the endpoint received the request
     */
    public WebhookRequest(
            final String url, final Iterable<? extends Map.Entry<String, String>> headers, final byte[] body,
            final Instant receivedAt) {
        this.url = url == null ? "" : url;
        final List<Map.Entry<String, String>> lowerCased = new ArrayList<>();
        if (headers != null) {
            for (Map.Entry<String, String> header : headers) {
                lowerCased.add(new AbstractMap.SimpleImmutableEntry<>(
                        header.getKey().toLowerCase(Locale.ROOT), header.getValue()));
            }
        }
        this.headers = Collections.unmodifiableList(lowerCased);
        this.body = body == null ? new byte[0] : body.clone();
        this.receivedAt = receivedAt == null ? Instant.now() : receivedAt;
    }

    /**
     * Returns the raw request target.
     *
     * @return the path and query
     */
    public String getUrl() {
        return url;
    }

    /**
     * Returns the header pairs in arrival order with lower-case names.
     *
     * @return the unmodifiable headers
     */
    public List<Map.Entry<String, String>> getHeaders() {
        return headers;
    }

    /**
     * Returns a copy of the complete raw body.
     *
     * @return the body bytes
     */
    public byte[] getBody() {
        return body.clone();
    }

    /**
     * Returns when the endpoint received the request.
     *
     * @return the receipt time
     */
    public Instant getReceivedAt() {
        return receivedAt;
    }

    /**
     * Returns the first value of a header, matched case-insensitively.
     *
     * @param name the header name
     * @return the value, or {@code null}
     */
    public String header(final String name) {
        final String wanted = name.toLowerCase(Locale.ROOT);
        for (Map.Entry<String, String> header : headers) {
            if (header.getKey().equals(wanted)) {
                return header.getValue();
            }
        }
        return null;
    }

    /**
     * Returns every value of a header in arrival order, matched case-insensitively.
     *
     * @param name the header name
     * @return the values
     */
    public List<String> headerValues(final String name) {
        final String wanted = name.toLowerCase(Locale.ROOT);
        final List<String> values = new ArrayList<>();
        for (Map.Entry<String, String> header : headers) {
            if (header.getKey().equals(wanted)) {
                values.add(header.getValue());
            }
        }
        return values;
    }

    /** {@inheritDoc} */
    @Override
    public String toString() {
        return "WebhookRequest{url=" + url + ", bodyBytes=" + body.length + "}";
    }
}
