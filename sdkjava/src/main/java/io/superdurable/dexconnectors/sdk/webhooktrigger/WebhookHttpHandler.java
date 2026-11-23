// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.webhooktrigger;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.URI;
import java.util.AbstractMap;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * Serves a webhook endpoint or endpoint runner through the JDK {@code com.sun.net.httpserver}
 * server.
 *
 * <pre>{@code
 * HttpServer server = HttpServer.create(new InetSocketAddress(8080), 0);
 * server.createContext("/webhooks/sample", new WebhookHttpHandler(runner));
 * server.setExecutor(Executors.newCachedThreadPool());
 * server.start();
 * }</pre>
 *
 * <p>Use an executor with several threads: each request waits for its arrival turn.
 */
public final class WebhookHttpHandler implements HttpHandler {
    private final WebhookEndpoint<?, ?> endpoint;
    private final WebhookEndpointRunner runner;

    /**
     * Serves an endpoint directly.
     *
     * @param endpoint the endpoint
     */
    public WebhookHttpHandler(final WebhookEndpoint<?, ?> endpoint) {
        if (endpoint == null) {
            throw new IllegalArgumentException("webhook endpoint is required");
        }
        this.endpoint = endpoint;
        this.runner = null;
    }

    /**
     * Serves an endpoint runner, answering 503 while its runners are restarting.
     *
     * @param runner the endpoint runner
     */
    public WebhookHttpHandler(final WebhookEndpointRunner runner) {
        if (runner == null) {
            throw new IllegalArgumentException("webhook endpoint runner is required");
        }
        this.endpoint = runner.endpoint();
        this.runner = runner;
    }

    /**
     * Answers one exchange.
     *
     * @param exchange the HTTP exchange
     * @throws IOException if the exchange cannot be read or written
     */
    @Override
    public void handle(final HttpExchange exchange) throws IOException {
        try {
            send(exchange, respond(exchange));
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            send(exchange, WebhookResponse.text(503, "webhook delivery temporarily unavailable"));
        } finally {
            exchange.close();
        }
    }

    private WebhookResponse respond(final HttpExchange exchange) throws IOException, InterruptedException {
        final String method = exchange.getRequestMethod();
        if (!"POST".equals(method.toUpperCase(Locale.ROOT))) {
            return WebhookResponse.text(405, "method not allowed", "allow", "POST");
        }
        final int maximumBodyBytes = endpoint.config().getMaximumBodyBytes();
        final byte[] body = readBody(exchange.getRequestBody(), maximumBodyBytes);
        if (body == null) {
            return WebhookResponse.text(413, "request body too large", "connection", "close");
        }
        if (runner != null && runner.getRunFailure() != null) {
            return WebhookResponse.text(503, "webhook delivery temporarily unavailable");
        }
        final List<Map.Entry<String, String>> headers = new ArrayList<>();
        for (Map.Entry<String, List<String>> header : exchange.getRequestHeaders().entrySet()) {
            for (String value : header.getValue()) {
                headers.add(new AbstractMap.SimpleImmutableEntry<>(header.getKey(), value));
            }
        }
        final URI uri = exchange.getRequestURI();
        final String target = uri.getRawPath() + (uri.getRawQuery() == null ? "" : "?" + uri.getRawQuery());
        return endpoint.handleRequest(method, target, headers, body, null);
    }

    private static byte[] readBody(final InputStream input, final int maximumBodyBytes) throws IOException {
        final ByteArrayOutputStream body = new ByteArrayOutputStream();
        final byte[] buffer = new byte[8192];
        int read;
        while ((read = input.read(buffer)) >= 0) {
            if (body.size() + read > maximumBodyBytes) {
                return null;
            }
            body.write(buffer, 0, read);
        }
        return body.toByteArray();
    }

    private static void send(final HttpExchange exchange, final WebhookResponse response) throws IOException {
        for (Map.Entry<String, String> header : response.getHeaders()) {
            exchange.getResponseHeaders().add(header.getKey(), header.getValue());
        }
        final byte[] body = response.getBody();
        exchange.sendResponseHeaders(response.getStatus(), body.length == 0 ? -1 : body.length);
        if (body.length > 0) {
            try (OutputStream output = exchange.getResponseBody()) {
                output.write(body);
            }
        }
    }
}
