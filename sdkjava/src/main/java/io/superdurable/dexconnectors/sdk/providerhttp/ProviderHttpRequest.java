// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.time.Duration;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * One provider HTTP request.
 *
 * <p>Instances are immutable. {@link #toString()} omits headers and the body, which may contain
 * credentials.
 */
public final class ProviderHttpRequest {
    private final String method;
    private final String url;
    private final Map<String, String> headers;
    private final byte[] body;
    private final Duration timeout;

    private ProviderHttpRequest(final Builder builder) {
        this.method = builder.method;
        this.url = builder.url;
        this.headers = Collections.unmodifiableMap(new LinkedHashMap<>(builder.headers));
        this.body = builder.body == null ? null : builder.body.clone();
        this.timeout = builder.timeout;
    }

    /**
     * Starts a request.
     *
     * @param method the HTTP method, such as {@code GET} or {@code POST}
     * @param url the absolute URL
     * @return a new builder
     */
    public static Builder newBuilder(final String method, final String url) {
        return new Builder(method, url);
    }

    /**
     * Returns the HTTP method.
     *
     * @return the method
     */
    public String getMethod() {
        return method;
    }

    /**
     * Returns the absolute URL.
     *
     * @return the URL
     */
    public String getUrl() {
        return url;
    }

    /**
     * Returns the request headers in insertion order.
     *
     * @return the unmodifiable headers
     */
    public Map<String, String> getHeaders() {
        return headers;
    }

    /**
     * Returns a copy of the request body.
     *
     * @return the body, or {@code null} for none
     */
    public byte[] getBody() {
        return body == null ? null : body.clone();
    }

    /**
     * Returns the per-request timeout that overrides the client default.
     *
     * @return the timeout, or {@code null} for the client default
     */
    public Duration getTimeout() {
        return timeout;
    }

    /**
     * Returns the method only, omitting the URL, headers, and body.
     *
     * @return the redacted description
     */
    @Override
    public String toString() {
        return "ProviderHttpRequest{" + method + "}";
    }

    /**
     * Builds a {@link ProviderHttpRequest}.
     */
    public static final class Builder {
        private final String method;
        private final String url;
        private final Map<String, String> headers = new LinkedHashMap<>();
        private byte[] body;
        private Duration timeout;

        private Builder(final String method, final String url) {
            if (method == null || method.isEmpty() || url == null || url.isEmpty()) {
                throw new IllegalArgumentException("request method and URL are required");
            }
            this.method = method;
            this.url = url;
        }

        /**
         * Sets one header, replacing an earlier value with the same name.
         *
         * @param name the header name
         * @param value the header value
         * @return this builder
         */
        public Builder header(final String name, final String value) {
            headers.put(name, value);
            return this;
        }

        /**
         * Sets the request body.
         *
         * @param value the body bytes
         * @return this builder
         */
        public Builder body(final byte[] value) {
            this.body = value == null ? null : value.clone();
            return this;
        }

        /**
         * Overrides the client's request timeout for this request.
         *
         * @param value the positive timeout
         * @return this builder
         */
        public Builder timeout(final Duration value) {
            if (value != null && (value.isZero() || value.isNegative())) {
                throw new IllegalArgumentException("provider request timeout must be positive");
            }
            this.timeout = value;
            return this;
        }

        /**
         * Builds the request.
         *
         * @return the request
         */
        public ProviderHttpRequest build() {
            return new ProviderHttpRequest(this);
        }
    }
}
