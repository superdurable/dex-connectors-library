// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.AddressRefusalException;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttp;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpRequest;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpResponse;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;
import io.superdurable.dexconnectors.sdk.providerhttp.SplitUrl;
import java.io.IOException;
import java.net.URI;
import java.net.URISyntaxException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Base64;
import java.util.Collections;
import java.util.Deque;
import java.util.HashSet;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * OAuth token endpoint and connector-owned response rules.
 *
 * <p>The transport belongs to the caller. Redirects are never followed. A declared terminal error
 * code below HTTP 500 throws {@link ReauthorizationRequiredException}; transport failures and other
 * provider errors throw {@link IOException}, and a malformed success response throws
 * {@link IllegalArgumentException}. Instances are immutable and thread-safe.
 */
public final class TokenEndpoint {
    /** Largest token response read, in bytes. */
    public static final int MAX_TOKEN_RESPONSE_BYTES = 1 << 20;

    private static final Set<String> STANDARD_RESPONSE_FIELDS = new HashSet<>(Arrays.asList(
            "access_token", "refresh_token", "expires_in", "refresh_token_expires_in", "scope",
            "token_type", "error", "error_description", "error_uri", "id_token"));
    private static final Set<String> GRANT_REQUEST_FIELDS = new HashSet<>(Arrays.asList(
            "grant_type", "refresh_token", "client_id", "client_secret", "assertion",
            "client_assertion", "client_assertion_type"));
    private static final ObjectMapper MAPPER = new ObjectMapper()
            .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);

    private final String providerName;
    private final String url;
    private final ProviderHttpTransport transport;
    private final List<String> terminalErrorCodes;
    private final List<String> acceptedTokenTypes;
    private final boolean acceptsMissingTokenType;
    private final boolean acceptsMissingExpiresIn;
    private final List<String> retainedResponseFields;
    private final boolean usesJsonRequestBody;
    private final boolean allowsPlainHttp;
    private final Map<String, String> additionalRequestParameters;
    private final Duration exchangeTimeout;

    private TokenEndpoint(final Builder builder) {
        this.providerName = builder.providerName;
        this.url = builder.url;
        this.transport = builder.transport;
        this.terminalErrorCodes = Collections.unmodifiableList(new ArrayList<>(builder.terminalErrorCodes));
        this.acceptedTokenTypes = Collections.unmodifiableList(new ArrayList<>(builder.acceptedTokenTypes));
        this.acceptsMissingTokenType = builder.acceptsMissingTokenType;
        this.acceptsMissingExpiresIn = builder.acceptsMissingExpiresIn;
        this.retainedResponseFields = Collections.unmodifiableList(new ArrayList<>(builder.retainedResponseFields));
        this.usesJsonRequestBody = builder.usesJsonRequestBody;
        this.allowsPlainHttp = builder.allowsPlainHttp;
        this.additionalRequestParameters = Collections.unmodifiableMap(
                new LinkedHashMap<>(builder.additionalRequestParameters));
        this.exchangeTimeout = builder.exchangeTimeout;
    }

    /**
     * Starts a token endpoint configuration.
     *
     * @param providerName the provider name used in secret-safe errors
     * @param url the absolute HTTPS token endpoint URL
     * @param transport the caller-owned HTTP transport
     * @return a new builder
     */
    public static Builder newBuilder(final String providerName, final String url, final ProviderHttpTransport transport) {
        return new Builder(providerName, url, transport);
    }

    /**
     * Exchanges a refresh token; missing grant material requires reauthorization.
     *
     * @param client the OAuth client
     * @param refreshToken the stored refresh token
     * @return the token response
     * @throws IOException if the endpoint is unavailable or returned an undeclared error
     * @throws ReauthorizationRequiredException if material is missing or the error is terminal
     */
    public TokenResponse exchangeRefreshToken(final ClientCredentials client, final SecretString refreshToken)
            throws IOException {
        if (client.getId().isEmpty() || refreshToken.isEmpty()) {
            throw new ReauthorizationRequiredException();
        }
        final Map<String, String> parameters = new LinkedHashMap<>();
        parameters.put("grant_type", "refresh_token");
        parameters.put("refresh_token", refreshToken.reveal());
        final ClientCredentials basicClient = applyClientAuthentication(parameters, client, true);
        return exchange(parameters, basicClient);
    }

    /**
     * Requests a client-credentials grant with optional space-separated scopes.
     *
     * @param client the OAuth client
     * @param scopes the requested scopes, possibly empty
     * @return the token response
     * @throws IOException if the endpoint is unavailable or returned an undeclared error
     * @throws ReauthorizationRequiredException if material is missing or the error is terminal
     */
    public TokenResponse exchangeClientCredentials(final ClientCredentials client, final List<String> scopes)
            throws IOException {
        if (client.getId().isEmpty() || client.getSecret().isEmpty()) {
            throw new ReauthorizationRequiredException();
        }
        final Map<String, String> parameters = new LinkedHashMap<>();
        parameters.put("grant_type", "client_credentials");
        if (!scopes.isEmpty()) {
            parameters.put("scope", String.join(" ", scopes));
        }
        final ClientCredentials basicClient = applyClientAuthentication(parameters, client, false);
        return exchange(parameters, basicClient);
    }

    /**
     * Exchanges a signed RFC 7523 JWT-bearer assertion without client material.
     *
     * @param assertion the compact signed assertion
     * @return the token response
     * @throws IOException if the endpoint is unavailable or returned an undeclared error
     * @throws ReauthorizationRequiredException if the assertion is empty or the error is terminal
     */
    public TokenResponse exchangeJwtBearerAssertion(final SecretString assertion) throws IOException {
        if (assertion.isEmpty()) {
            throw new ReauthorizationRequiredException();
        }
        final Map<String, String> parameters = new LinkedHashMap<>();
        parameters.put("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer");
        parameters.put("assertion", assertion.reveal());
        return exchange(parameters, null);
    }

    private ClientCredentials applyClientAuthentication(
            final Map<String, String> parameters, final ClientCredentials client, final boolean allowsPublic) {
        if (client.getAuthenticationMethod() == ClientAuthenticationMethod.PUBLIC_CLIENT && allowsPublic) {
            parameters.put("client_id", client.getId());
            return null;
        }
        if (client.getAuthenticationMethod() == ClientAuthenticationMethod.PUBLIC_CLIENT) {
            throw new IllegalArgumentException("OAuth client authentication method is not supported");
        }
        if (client.getSecret().isEmpty()) {
            throw new ReauthorizationRequiredException();
        }
        if (client.getAuthenticationMethod() == ClientAuthenticationMethod.CLIENT_SECRET_POST) {
            parameters.put("client_id", client.getId());
            parameters.put("client_secret", client.getSecret().reveal());
            return null;
        }
        return client;
    }

    private TokenResponse exchange(final Map<String, String> parameters, final ClientCredentials basicClient)
            throws IOException {
        validateUrl();
        for (String name : retainedResponseFields) {
            if (STANDARD_RESPONSE_FIELDS.contains(name)) {
                throw new IllegalArgumentException("OAuth token endpoint cannot retain standard token fields");
            }
        }
        for (String name : additionalRequestParameters.keySet()) {
            if (GRANT_REQUEST_FIELDS.contains(name)) {
                throw new IllegalArgumentException("OAuth token endpoint cannot replace grant parameters");
            }
        }
        parameters.putAll(additionalRequestParameters);
        final ProviderHttpRequest.Builder request = ProviderHttpRequest.newBuilder("POST", url)
                .header("Accept", "application/json");
        if (usesJsonRequestBody) {
            request.header("Content-Type", "application/json");
            try {
                request.body(MAPPER.writeValueAsBytes(parameters));
            } catch (JsonProcessingException error) {
                throw new IllegalStateException("token request parameters are not encodable", error);
            }
        } else {
            request.header("Content-Type", "application/x-www-form-urlencoded");
            final StringBuilder form = new StringBuilder();
            for (Map.Entry<String, String> parameter : new TreeMap<>(parameters).entrySet()) {
                if (form.length() > 0) {
                    form.append('&');
                }
                form.append(ProviderHttp.encodeFormComponent(parameter.getKey())).append('=')
                        .append(ProviderHttp.encodeFormComponent(parameter.getValue()));
            }
            request.body(form.toString().getBytes(StandardCharsets.UTF_8));
        }
        if (basicClient != null) {
            final String userPass = basicClient.getId() + ":" + basicClient.getSecret().reveal();
            request.header("Authorization",
                    "Basic " + Base64.getEncoder().encodeToString(userPass.getBytes(StandardCharsets.UTF_8)));
        }
        if (exchangeTimeout != null) {
            request.timeout(exchangeTimeout);
        }
        final ProviderHttpRequest built = request.build();
        final ResponseBody response = exchangeTimeout == null ? send(built) : sendWithin(built, exchangeTimeout);
        return decode(response.statusCode, response.contents);
    }

    private void validateUrl() {
        final String lowered = url.toLowerCase(Locale.ROOT);
        final boolean hasAllowedPrefix = lowered.startsWith("https://") || (allowsPlainHttp && lowered.startsWith("http://"));
        final SplitUrl parsed = SplitUrl.split(url);
        final boolean hasAllowedScheme = "https".equals(parsed.getScheme())
                || (allowsPlainHttp && "http".equals(parsed.getScheme()));
        if (!hasAllowedPrefix || !hasAllowedScheme || parsed.getHostname().isEmpty() || parsed.hasUserInfo()) {
            throw new IllegalArgumentException("OAuth token endpoint URL is invalid");
        }
        try {
            final URI uri = new URI(url);
            if (uri.getHost() == null) {
                throw new InvalidTokenEndpointUrlException();
            }
        } catch (URISyntaxException error) {
            throw new InvalidTokenEndpointUrlException();
        }
    }

    private ResponseBody send(final ProviderHttpRequest request) throws IOException {
        try {
            final ProviderHttpResponse response = transport.send(request);
            return new ResponseBody(response.getStatusCode(), response.readBoundedBody(MAX_TOKEN_RESPONSE_BYTES));
        } catch (AddressRefusalException error) {
            throw error;
        } catch (IOException | IllegalArgumentException error) {
            throw new IOException(providerName + " token endpoint is unavailable");
        }
    }

    private ResponseBody sendWithin(final ProviderHttpRequest request, final Duration timeout) throws IOException {
        final ExecutorService executor = Executors.newSingleThreadExecutor(runnable -> {
            final Thread thread = new Thread(runnable, "dex-connector-token-exchange");
            thread.setDaemon(true);
            return thread;
        });
        try {
            final Future<ResponseBody> future = executor.submit(() -> send(request));
            try {
                return future.get(timeout.toNanos(), TimeUnit.NANOSECONDS);
            } catch (TimeoutException error) {
                future.cancel(true);
                throw new IOException("token endpoint is unavailable");
            } catch (InterruptedException error) {
                future.cancel(true);
                Thread.currentThread().interrupt();
                throw new IOException("token endpoint is unavailable");
            } catch (ExecutionException error) {
                final Throwable cause = error.getCause();
                if (cause instanceof IOException) {
                    throw (IOException) cause;
                }
                if (cause instanceof RuntimeException) {
                    throw (RuntimeException) cause;
                }
                throw new IOException("token endpoint is unavailable");
            }
        } finally {
            executor.shutdownNow();
        }
    }

    private TokenResponse decode(final int statusCode, final byte[] contents) throws IOException {
        final boolean isSuccess = statusCode >= 200 && statusCode < 300;
        if (!isSuccess && contents.length == 0) {
            throw new IOException(providerName + " token endpoint returned HTTP " + statusCode);
        }
        final JsonNode decoded;
        try {
            decoded = MAPPER.readTree(contents);
        } catch (IOException error) {
            throw new IllegalArgumentException(providerName + " token response is invalid");
        }
        if (decoded == null || !decoded.isObject()) {
            throw new IllegalArgumentException(providerName + " token response is invalid");
        }
        final JsonNode errorNode = decoded.get("error");
        final String errorCode = errorNode != null && errorNode.isTextual() ? errorNode.textValue() : "";
        if (!isSuccess || !errorCode.isEmpty()) {
            if (statusCode < 500 && terminalErrorCodes.contains(errorCode)) {
                throw new ReauthorizationRequiredException();
            }
            throw new IOException(providerName + " token endpoint returned HTTP " + statusCode);
        }
        final JsonNode accessToken = decoded.get("access_token");
        final JsonNode expiresIn = decoded.has("expires_in") ? decoded.get("expires_in") : null;
        final JsonNode tokenType = decoded.get("token_type");
        final List<String> acceptedTypes = acceptedTokenTypes.isEmpty()
                ? Collections.singletonList("Bearer") : acceptedTokenTypes;
        final long expiresInSeconds = expiresIn == null ? 0
                : isPlainInteger(expiresIn) ? expiresIn.longValue() : Long.MIN_VALUE;
        final String tokenTypeText = tokenType == null ? "" : tokenType.isTextual() ? tokenType.textValue() : null;
        if (accessToken == null || !accessToken.isTextual() || accessToken.textValue().isEmpty()
                || expiresInSeconds == Long.MIN_VALUE
                || (expiresInSeconds <= 0 && !(expiresInSeconds == 0 && acceptsMissingExpiresIn))
                || tokenTypeText == null
                || (tokenTypeText.isEmpty() && !acceptsMissingTokenType)
                || (!tokenTypeText.isEmpty() && !containsIgnoringCase(acceptedTypes, tokenTypeText))) {
            throw new IllegalArgumentException(providerName + " token response omitted required fields");
        }
        final JsonNode refreshToken = decoded.get("refresh_token");
        final JsonNode refreshExpiry = decoded.get("refresh_token_expires_in");
        final JsonNode scope = decoded.get("scope");
        if ((refreshToken != null && !refreshToken.isTextual())
                || (refreshExpiry != null && !isPlainInteger(refreshExpiry))
                || (scope != null && !scope.isTextual())) {
            throw new IllegalArgumentException(providerName + " token response is invalid");
        }
        final Map<String, JsonNode> retained = new LinkedHashMap<>();
        for (String name : retainedResponseFields) {
            if (decoded.has(name)) {
                retained.put(name, decoded.get(name));
            }
        }
        return new TokenResponse(
                new SecretString(accessToken.textValue()),
                new SecretString(refreshToken == null ? "" : refreshToken.textValue()),
                Duration.ofSeconds(expiresInSeconds),
                Duration.ofSeconds(Math.max(0, refreshExpiry == null ? 0 : refreshExpiry.longValue())),
                scope == null ? "" : scope.textValue(),
                tokenTypeText, retained, redactResponseStrings(decoded));
    }

    private static Map<String, SecretString> redactResponseStrings(final JsonNode response) {
        final Map<String, SecretString> redacted = new LinkedHashMap<>();
        final Deque<Object[]> pending = new ArrayDeque<>();
        pending.push(new Object[] {"", response});
        while (!pending.isEmpty()) {
            final Object[] entry = pending.pop();
            final String path = (String) entry[0];
            final JsonNode value = (JsonNode) entry[1];
            if (value.isTextual()) {
                redacted.put(path, new SecretString(value.textValue()));
            } else if (value.isObject()) {
                final Iterator<Map.Entry<String, JsonNode>> fields = value.fields();
                while (fields.hasNext()) {
                    final Map.Entry<String, JsonNode> field = fields.next();
                    pending.push(new Object[] {path.isEmpty() ? field.getKey() : path + "." + field.getKey(),
                        field.getValue()});
                }
            }
        }
        return redacted;
    }

    private static boolean isPlainInteger(final JsonNode value) {
        return value.isIntegralNumber() && value.canConvertToLong();
    }

    private static boolean containsIgnoringCase(final List<String> values, final String candidate) {
        for (String value : values) {
            if (value.toLowerCase(Locale.ROOT).equals(candidate.toLowerCase(Locale.ROOT))) {
                return true;
            }
        }
        return false;
    }

    private static final class ResponseBody {
        private final int statusCode;
        private final byte[] contents;

        private ResponseBody(final int statusCode, final byte[] contents) {
            this.statusCode = statusCode;
            this.contents = contents;
        }
    }

    /** The endpoint URL passed the token URL rules but cannot be dialed. */
    static final class InvalidTokenEndpointUrlException extends IllegalArgumentException {
        private static final long serialVersionUID = 1L;

        InvalidTokenEndpointUrlException() {
            super("token endpoint is invalid");
        }
    }

    /**
     * Builds a {@link TokenEndpoint}.
     */
    public static final class Builder {
        private final String providerName;
        private final String url;
        private final ProviderHttpTransport transport;
        private List<String> terminalErrorCodes = Collections.emptyList();
        private List<String> acceptedTokenTypes = Collections.emptyList();
        private boolean acceptsMissingTokenType;
        private boolean acceptsMissingExpiresIn;
        private List<String> retainedResponseFields = Collections.emptyList();
        private boolean usesJsonRequestBody;
        private boolean allowsPlainHttp;
        private Map<String, String> additionalRequestParameters = Collections.emptyMap();
        private Duration exchangeTimeout;

        private Builder(final String providerName, final String url, final ProviderHttpTransport transport) {
            if (providerName == null || providerName.isEmpty() || url == null || transport == null) {
                throw new IllegalArgumentException("provider name, token URL, and transport are required");
            }
            this.providerName = providerName;
            this.url = url;
            this.transport = transport;
        }

        /**
         * Sets provider error codes that require interactive reauthorization.
         *
         * @param value the terminal error codes
         * @return this builder
         */
        public Builder terminalErrorCodes(final List<String> value) {
            this.terminalErrorCodes = value;
            return this;
        }

        /**
         * Sets accepted token types, compared case-insensitively; empty accepts {@code Bearer}.
         *
         * @param value the accepted token types
         * @return this builder
         */
        public Builder acceptedTokenTypes(final List<String> value) {
            this.acceptedTokenTypes = value;
            return this;
        }

        /**
         * Accepts a response without {@code token_type}.
         *
         * @param value whether a missing token type is accepted
         * @return this builder
         */
        public Builder acceptsMissingTokenType(final boolean value) {
            this.acceptsMissingTokenType = value;
            return this;
        }

        /**
         * Accepts a response without {@code expires_in}.
         *
         * @param value whether a missing expiry is accepted
         * @return this builder
         */
        public Builder acceptsMissingExpiresIn(final boolean value) {
            this.acceptsMissingExpiresIn = value;
            return this;
        }

        /**
         * Retains nonstandard top-level response fields in {@link TokenResponse#getRetainedFields()}.
         *
         * @param value the field names
         * @return this builder
         */
        public Builder retainedResponseFields(final List<String> value) {
            this.retainedResponseFields = value;
            return this;
        }

        /**
         * Sends a JSON body instead of a form body.
         *
         * @param value whether to use JSON
         * @return this builder
         */
        public Builder usesJsonRequestBody(final boolean value) {
            this.usesJsonRequestBody = value;
            return this;
        }

        /**
         * Allows a plain {@code http://} endpoint, as for a private-network token service.
         *
         * @param value whether plain HTTP is allowed
         * @return this builder
         */
        public Builder allowsPlainHttp(final boolean value) {
            this.allowsPlainHttp = value;
            return this;
        }

        /**
         * Adds provider-specific request parameters that never replace grant parameters.
         *
         * @param value the parameters
         * @return this builder
         */
        public Builder additionalRequestParameters(final Map<String, String> value) {
            this.additionalRequestParameters = value;
            return this;
        }

        /**
         * Bounds the complete exchange, including the response body read.
         *
         * @param value the positive timeout, or {@code null} for the transport's own timeout
         * @return this builder
         */
        public Builder exchangeTimeout(final Duration value) {
            this.exchangeTimeout = value;
            return this;
        }

        /**
         * Builds the endpoint.
         *
         * @return the endpoint
         */
        public TokenEndpoint build() {
            return new TokenEndpoint(this);
        }
    }
}
