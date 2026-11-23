// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.Arrays;

/**
 * Sends one credential broker JSON POST; replace it in tests or to customize transport.
 *
 * <p>Implementations do not follow redirects. A transport failure throws an {@link IOException};
 * the broker client reports it as {@link CredentialBrokerUnavailableException}.
 */
public interface BrokerHttpTransport {
    /**
     * Posts a JSON body with a bearer scope token.
     *
     * @param url the absolute target URL
     * @param bearerToken the scope token
     * @param body the JSON request body
     * @param maximumResponseBytes the largest accepted 200 response body
     * @return the status and, for 200, the body
     * @throws IOException if the transport fails
     */
    Response post(String url, String bearerToken, byte[] body, int maximumResponseBytes) throws IOException;

    /**
     * Returns the default transport built on {@link HttpURLConnection} with 40-second timeouts.
     *
     * @return the transport
     */
    static BrokerHttpTransport httpUrlConnection() {
        return (url, bearerToken, body, maximumResponseBytes) -> {
            final HttpURLConnection connection = (HttpURLConnection) new URL(url).openConnection();
            try {
                connection.setInstanceFollowRedirects(false);
                connection.setConnectTimeout(40_000);
                connection.setReadTimeout(40_000);
                connection.setRequestMethod("POST");
                connection.setDoOutput(true);
                connection.setRequestProperty("Authorization", "Bearer " + bearerToken);
                connection.setRequestProperty("Content-Type", "application/json");
                connection.setRequestProperty("Accept", "application/json");
                try (OutputStream output = connection.getOutputStream()) {
                    output.write(body);
                }
                final int status = connection.getResponseCode();
                if (status != 200) {
                    return new Response(status, new byte[0]);
                }
                try (InputStream input = connection.getInputStream()) {
                    final ByteArrayOutputStream contents = new ByteArrayOutputStream();
                    final byte[] buffer = new byte[8192];
                    int read;
                    while ((read = input.read(buffer)) >= 0) {
                        contents.write(buffer, 0, read);
                        if (contents.size() > maximumResponseBytes) {
                            throw new CredentialBrokerRejectedException("credential broker response is too large");
                        }
                    }
                    return new Response(status, contents.toByteArray());
                }
            } finally {
                connection.disconnect();
            }
        };
    }

    /**
     * One broker HTTP response.
     */
    final class Response {
        private final int statusCode;
        private final byte[] body;

        /**
         * Creates a response.
         *
         * @param statusCode the HTTP status code
         * @param body the response body
         */
        public Response(final int statusCode, final byte[] body) {
            this.statusCode = statusCode;
            this.body = body == null ? new byte[0] : body.clone();
        }

        /**
         * Returns the HTTP status code.
         *
         * @return the status
         */
        public int getStatusCode() {
            return statusCode;
        }

        /**
         * Returns a copy of the body.
         *
         * @return the body
         */
        public byte[] getBody() {
            return Arrays.copyOf(body, body.length);
        }
    }
}
