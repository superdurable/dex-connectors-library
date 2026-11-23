// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.net.URI;
import java.net.URISyntaxException;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.OffsetDateTime;

/**
 * Scope-token client of the shared credential read protocol.
 *
 * <p>A cached scope token is used without waiting for a reread. A 401 answer rereads the current
 * token reference once and retries; an unchanged token is a terminal rejection. Thread-safe.
 * Relayed request signature verification is not part of this client yet.
 */
public final class CredentialBrokerClient {
    /** Largest broker response body in bytes. */
    public static final int MAXIMUM_BROKER_RESPONSE_BYTES = 1 << 20;
    /** Largest READY credential body in bytes. */
    public static final int MAXIMUM_BROKER_CREDENTIAL_BYTES = 64 << 10;

    private static final int MAXIMUM_RELAYED_TRIGGER_REGISTRATION_BYTES = 4096;

    private final String endpoint;
    private final SecretTokenReader readToken;
    private final TokenReferenceSupplier currentTokenReference;
    private final BrokerHttpTransport transport;
    private final Object tokenLock = new Object();
    private volatile ApplicationSecretRef tokenReference;
    private volatile String token = "";

    /**
     * Binds the trusted endpoint, scope-token readers, and HTTP transport.
     *
     * @param configuration the broker configuration
     * @param readToken reads the scope token of a reference
     * @param currentTokenReference returns the current token reference after rotation
     * @param transport the HTTP transport
     * @throws IllegalArgumentException if the endpoint is invalid or a dependency is missing
     */
    public CredentialBrokerClient(
            final CredentialBrokerConfiguration configuration, final SecretTokenReader readToken,
            final TokenReferenceSupplier currentTokenReference, final BrokerHttpTransport transport) {
        if (configuration == null || readToken == null || currentTokenReference == null || transport == null) {
            throw new IllegalArgumentException("credential broker configuration, token readers, and transport are required");
        }
        ProjectConfigurationRules.validateBrokerEndpoint(configuration.getEndpoint());
        this.endpoint = configuration.getEndpoint();
        this.tokenReference = configuration.getTokenSecretRef();
        this.readToken = readToken;
        this.currentTokenReference = currentTokenReference;
        this.transport = transport;
    }

    /**
     * Reads one shared credential, renewing a rotated scope token once on 401.
     *
     * @param sharedConnectionId the shared connection ID
     * @param rejectedRevision the revision a provider rejected, or 0
     * @return the credential
     * @throws CredentialBrokerUnavailableException for a transient failure
     * @throws CredentialBrokerRejectedException for a terminal refusal or invalid answer
     * @throws ConnectionNotSelectedException if the scope no longer selects the connection
     * @throws ReauthorizationRequiredException if the connection requires reauthorization
     * @throws IOException if the scope token cannot be read
     */
    public BrokerCredential read(final String sharedConnectionId, final long rejectedRevision) throws IOException {
        if (sharedConnectionId == null || sharedConnectionId.isEmpty() || rejectedRevision < 0) {
            throw new IllegalArgumentException("invalid shared credential read");
        }
        final ObjectNode request = GoCompatibleJson.newObject();
        request.put("sharedConnectionId", sharedConnectionId);
        if (rejectedRevision != 0) {
            request.put("rejectedRevision", rejectedRevision);
        }
        return decode(exchange(endpoint, request));
    }

    /**
     * Registers a shared connection's relayed Trigger route on the broker endpoint's origin.
     *
     * @param sharedConnectionId the shared connection ID
     * @param connectionName the application connection name
     * @param registrationPath the absolute path on the broker origin
     * @throws ConnectionNotSelectedException if the scope no longer selects the connection
     * @throws RelayedTriggerNotRelayedException if the broker does not relay this connection
     * @throws CredentialBrokerRejectedException for any other answer
     * @throws CredentialBrokerUnavailableException for a transient failure
     * @throws IOException if the scope token cannot be read
     */
    public void registerRelayedTrigger(
            final String sharedConnectionId, final String connectionName, final String registrationPath)
            throws IOException {
        final String target = originUrl(registrationPath);
        final ObjectNode request = GoCompatibleJson.newObject();
        request.put("sharedConnectionId", sharedConnectionId);
        request.put("connectionName", connectionName);
        if (GoCompatibleJson.encode(request).length > MAXIMUM_RELAYED_TRIGGER_REGISTRATION_BYTES) {
            throw new IllegalArgumentException("relayed Trigger registration request is too large");
        }
        final BrokerHttpTransport.Response response = exchange(target, request);
        final int status = response.getStatusCode();
        if (isTransient(status)) {
            throw new CredentialBrokerUnavailableException("credential broker HTTP " + status, null);
        }
        if (status != 200) {
            throw new CredentialBrokerRejectedException("credential broker HTTP " + status);
        }
        final JsonNode answer;
        try {
            answer = GoCompatibleJson.parse(response.getBody());
        } catch (IllegalArgumentException error) {
            throw new CredentialBrokerRejectedException("credential broker answer is invalid");
        }
        final String answerStatus = answer.isObject() ? ProjectConfigurationRules.text(answer, "status") : "";
        if ("REGISTERED".equals(answerStatus)) {
            return;
        }
        if ("CONNECTION_NOT_SELECTED".equals(answerStatus)) {
            throw new ConnectionNotSelectedException();
        }
        if ("NOT_RELAYED".equals(answerStatus)) {
            throw new RelayedTriggerNotRelayedException();
        }
        throw new CredentialBrokerRejectedException("credential broker status is unknown");
    }

    static BrokerCredential decode(final BrokerHttpTransport.Response response)
            throws CredentialBrokerUnavailableException {
        final int status = response.getStatusCode();
        if (isTransient(status)) {
            throw new CredentialBrokerUnavailableException("credential broker HTTP " + status, null);
        }
        if (status != 200) {
            throw new CredentialBrokerRejectedException("credential broker HTTP " + status);
        }
        final byte[] body = response.getBody();
        if (body.length > MAXIMUM_BROKER_RESPONSE_BYTES) {
            throw new CredentialBrokerRejectedException("credential broker response is too large");
        }
        final ObjectNode answer;
        try {
            answer = GoCompatibleJson.parseObject(body, "broker answer");
        } catch (IllegalArgumentException error) {
            throw new CredentialBrokerRejectedException("credential broker answer is invalid");
        }
        final String answerStatus = ProjectConfigurationRules.text(answer, "status");
        if ("REAUTHORIZATION_REQUIRED".equals(answerStatus)) {
            throw new ReauthorizationRequiredException();
        }
        if ("CONNECTION_NOT_SELECTED".equals(answerStatus)) {
            throw new ConnectionNotSelectedException();
        }
        if (!"READY".equals(answerStatus)) {
            throw new CredentialBrokerRejectedException("credential broker status is unknown");
        }
        final JsonNode credential = answer.get("credential");
        final JsonNode revision = answer.get("revision");
        if (credential == null || !credential.isObject() || revision == null || !revision.isIntegralNumber()
                || revision.longValue() <= 0
                || credential.toString().getBytes(StandardCharsets.UTF_8).length > MAXIMUM_BROKER_CREDENTIAL_BYTES) {
            throw new CredentialBrokerRejectedException("credential broker READY answer is invalid");
        }
        final OffsetDateTime issuedAt;
        final OffsetDateTime expiresAt;
        try {
            issuedAt = optionalTime(answer.get("issuedAt"));
            expiresAt = optionalTime(answer.get("expiresAt"));
        } catch (RuntimeException error) {
            throw new CredentialBrokerRejectedException("credential broker timestamps are invalid");
        }
        return new BrokerCredential((ObjectNode) credential, revision.longValue(), issuedAt, expiresAt, Instant.now());
    }

    private BrokerHttpTransport.Response exchange(final String targetUrl, final ObjectNode body) throws IOException {
        final String current = cachedOrReadToken();
        BrokerHttpTransport.Response response = post(targetUrl, current, body);
        if (response.getStatusCode() == 401) {
            final String renewed = renewToken(current);
            if (renewed.equals(current)) {
                throw new CredentialBrokerRejectedException("credential broker refused scope token");
            }
            response = post(targetUrl, renewed, body);
        }
        return response;
    }

    private String cachedOrReadToken() throws IOException {
        final String cached = token;
        if (!cached.isEmpty()) {
            return cached;
        }
        synchronized (tokenLock) {
            if (token.isEmpty()) {
                token = readToken.read(tokenReference);
            }
            return token;
        }
    }

    private String renewToken(final String refused) throws IOException {
        synchronized (tokenLock) {
            if (!token.isEmpty() && !token.equals(refused)) {
                return token;
            }
            token = "";
            tokenReference = currentTokenReference.current();
            token = readToken.read(tokenReference);
            return token;
        }
    }

    private BrokerHttpTransport.Response post(final String targetUrl, final String bearerToken, final ObjectNode body)
            throws IOException {
        try {
            return transport.post(targetUrl, bearerToken, GoCompatibleJson.encode(body), MAXIMUM_BROKER_RESPONSE_BYTES);
        } catch (CredentialBrokerUnavailableException error) {
            throw error;
        } catch (IOException error) {
            throw new CredentialBrokerUnavailableException("credential broker is unavailable", null);
        }
    }

    private String originUrl(final String path) {
        if (path == null || !path.startsWith("/") || path.startsWith("//") || path.indexOf('?') >= 0
                || path.indexOf('#') >= 0 || path.indexOf('\\') >= 0) {
            throw new IllegalArgumentException(
                    "credential broker endpoint path must be an absolute path without a query or fragment");
        }
        try {
            final URI parsed = new URI(endpoint);
            return parsed.getScheme() + "://" + parsed.getRawAuthority() + path;
        } catch (URISyntaxException error) {
            throw new IllegalArgumentException("credential broker endpoint is invalid", error);
        }
    }

    private static boolean isTransient(final int status) {
        return status == 408 || status == 425 || status == 429 || status >= 500;
    }

    private static OffsetDateTime optionalTime(final JsonNode value) {
        if (value == null || value.isNull() || (value.isTextual() && value.textValue().isEmpty())) {
            return null;
        }
        if (!value.isTextual()) {
            throw new IllegalArgumentException("timestamp must be text");
        }
        return Rfc3339NanoTimestamp.fromWire(value.textValue()).toOffsetDateTime();
    }

    /**
     * Reads the scope token of one pinned secret reference.
     */
    public interface SecretTokenReader {
        /**
         * Reads the token.
         *
         * @param reference the pinned secret reference
         * @return the scope token
         * @throws IOException if the secret cannot be read
         */
        String read(ApplicationSecretRef reference) throws IOException;
    }

    /**
     * Returns the current scope-token reference, which changes after rotation.
     */
    public interface TokenReferenceSupplier {
        /**
         * Returns the current reference.
         *
         * @return the reference
         * @throws IOException if the configuration cannot be read
         */
        ApplicationSecretRef current() throws IOException;
    }
}
