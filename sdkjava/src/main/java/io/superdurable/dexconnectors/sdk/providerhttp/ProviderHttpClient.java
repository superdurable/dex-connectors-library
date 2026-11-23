// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import io.superdurable.dexconnectors.sdk.singledispatch.DispatchObservation;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.MalformedURLException;
import java.net.Proxy;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.URL;
import java.net.URLConnection;
import java.time.Duration;
import java.time.Instant;
import java.util.Locale;
import java.util.Map;

/**
 * Redirect-safe provider HTTP client with an optional address policy.
 *
 * <p>The client never follows redirects and never caches. {@link AddressPolicy#UNRESTRICTED}
 * keeps the JVM's network behavior, including system proxies. A restricted policy checks every DNS
 * answer before dialing, refuses Unix-style and non-HTTP schemes, and bypasses proxies. The JDK
 * then dials the hostname itself, so the checked answers rely on the JVM DNS cache rather than a
 * pinned address; TLS verification and SNI use the original hostname.
 *
 * <p>Each request clears the enclosing single-dispatch observation before it starts and records
 * the response status when headers arrive, so a Fence write-window timeout reports the status of
 * the request it cut off. A DNS, TCP connect, or TLS handshake failure is provably not sent;
 * {@code HttpURLConnection} cannot send {@code PATCH}, so this client rejects it.
 *
 * <p>Instances are immutable and thread-safe.
 */
public final class ProviderHttpClient implements ProviderHttpTransport {
    private final Duration requestTimeout;
    private final AddressPolicy addressPolicy;

    /**
     * Creates a client.
     *
     * @param requestTimeout the positive connect and read timeout for each request
     * @param addressPolicy the address policy
     * @throws IllegalArgumentException if the timeout is not positive or the policy is missing
     */
    public ProviderHttpClient(final Duration requestTimeout, final AddressPolicy addressPolicy) {
        if (requestTimeout == null || requestTimeout.isZero() || requestTimeout.isNegative()) {
            throw new IllegalArgumentException("provider request timeout must be positive");
        }
        if (addressPolicy == null) {
            throw new IllegalArgumentException("address policy is invalid");
        }
        this.requestTimeout = requestTimeout;
        this.addressPolicy = addressPolicy;
    }

    /**
     * Creates an unrestricted client with a 30-second request timeout.
     */
    public ProviderHttpClient() {
        this(Duration.ofSeconds(30), AddressPolicy.UNRESTRICTED);
    }

    /**
     * Returns the default request timeout.
     *
     * @return the timeout
     */
    public Duration getRequestTimeout() {
        return requestTimeout;
    }

    /**
     * Returns the address policy.
     *
     * @return the policy
     */
    public AddressPolicy getAddressPolicy() {
        return addressPolicy;
    }

    /**
     * Sends a request once without following redirects and returns the streaming response.
     *
     * @param request the request
     * @return the response, owned by the caller
     * @throws AddressRefusalException if the address policy refused the request; nothing was sent
     * @throws ProviderTransportException if DNS, connect, TLS, or the exchange failed
     * @throws IOException if the request failed in another way
     */
    @Override
    public ProviderHttpResponse send(final ProviderHttpRequest request) throws IOException {
        final URI uri;
        try {
            uri = new URI(request.getUrl());
        } catch (URISyntaxException error) {
            throw new IllegalArgumentException("provider URL is invalid");
        }
        final String scheme = uri.getScheme() == null ? "" : uri.getScheme().toLowerCase(Locale.ROOT);
        if (!"http".equals(scheme) && !"https".equals(scheme)) {
            if (addressPolicy != AddressPolicy.UNRESTRICTED) {
                throw new AddressRefusalException("URL scheme is not allowed");
            }
            throw new IllegalArgumentException("provider URL scheme must be http or https");
        }
        if (uri.getHost() == null || uri.getHost().isEmpty()) {
            throw new IllegalArgumentException("provider URL is invalid");
        }
        final String method = request.getMethod().toUpperCase(Locale.ROOT);
        if ("PATCH".equals(method)) {
            throw new IllegalArgumentException("HttpURLConnection cannot send PATCH requests");
        }
        DispatchObservation.clearResponseStatus();
        final Duration timeout = boundByWriteDeadline(request.getTimeout() == null ? requestTimeout : request.getTimeout());
        if (addressPolicy != AddressPolicy.UNRESTRICTED) {
            final String host = uri.getHost().startsWith("[") && uri.getHost().endsWith("]")
                    ? uri.getHost().substring(1, uri.getHost().length() - 1) : uri.getHost();
            try {
                AddressPolicyRules.resolveCheckedAddresses(host, scheme, addressPolicy);
            } catch (java.net.UnknownHostException error) {
                throw new ProviderTransportException(true, null, null);
            }
        }
        final URL url;
        try {
            url = uri.toURL();
        } catch (MalformedURLException error) {
            throw new IllegalArgumentException("provider URL is invalid");
        }
        final URLConnection opened = addressPolicy == AddressPolicy.UNRESTRICTED
                ? url.openConnection() : url.openConnection(Proxy.NO_PROXY);
        final HttpURLConnection connection = (HttpURLConnection) opened;
        connection.setInstanceFollowRedirects(false);
        connection.setUseCaches(false);
        connection.setConnectTimeout(timeoutMillis(timeout));
        connection.setReadTimeout(timeoutMillis(timeout));
        connection.setRequestMethod(method);
        for (Map.Entry<String, String> header : request.getHeaders().entrySet()) {
            connection.setRequestProperty(header.getKey(), header.getValue());
        }
        final byte[] body = request.getBody();
        if (body != null) {
            connection.setDoOutput(true);
            connection.setFixedLengthStreamingMode(body.length);
        }
        // A Fence closes the connection at its write deadline, so blocked I/O cannot send late.
        DispatchObservation.registerCancellation(connection::disconnect);
        try {
            connection.connect();
        } catch (IOException error) {
            connection.disconnect();
            throw new ProviderTransportException(true, null, null);
        }
        final int statusCode;
        try {
            final Duration remaining = boundByWriteDeadline(timeout);
            connection.setReadTimeout(timeoutMillis(remaining));
            if (body != null) {
                try (OutputStream output = connection.getOutputStream()) {
                    output.write(body);
                }
            }
            statusCode = connection.getResponseCode();
        } catch (IOException error) {
            connection.disconnect();
            throw new ProviderTransportException(false, null, null);
        }
        if (statusCode < 0) {
            connection.disconnect();
            throw new ProviderTransportException(false, null, null);
        }
        DispatchObservation.recordResponseStatus(statusCode);
        final ProviderHttpHeaders headers = new ProviderHttpHeaders(connection.getHeaderFields());
        InputStream stream;
        try {
            stream = statusCode >= 400 ? connection.getErrorStream() : connection.getInputStream();
        } catch (IOException error) {
            stream = connection.getErrorStream();
        }
        return new ProviderHttpResponse(statusCode, headers, stream, connection::disconnect);
    }

    /**
     * Bounds a timeout by the remaining time of the enclosing Fence write window.
     *
     * @throws ProviderTransportException as provably not sent once the window has passed
     */
    private static Duration boundByWriteDeadline(final Duration timeout) throws ProviderTransportException {
        final Instant writeDeadline = DispatchObservation.currentWriteDeadline();
        if (writeDeadline == null) {
            return timeout;
        }
        final Duration remaining = Duration.between(Instant.now(), writeDeadline);
        if (remaining.isZero() || remaining.isNegative()) {
            throw new ProviderTransportException(true, null, null);
        }
        return remaining.compareTo(timeout) < 0 ? remaining : timeout;
    }

    private static int timeoutMillis(final Duration timeout) {
        final long millis = Math.max(1, timeout.toMillis());
        return millis > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) millis;
    }
}
