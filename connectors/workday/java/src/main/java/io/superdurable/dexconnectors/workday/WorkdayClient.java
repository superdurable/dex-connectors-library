// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.oauthtoken.ManifestCredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.*;
import java.net.URI;
import java.time.*;
import java.util.*;

/** Shared binding for bounded Workday Common REST reads and renewable OAuth credentials. */
public final class WorkdayClient {
    static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final JsonNode DEFINITIONS = readResource("operation-definitions.json");
    private static final JsonNode AUTHORIZATION = readResource("refresh-authorization.json");
    private final CredentialProvider<WorkdayCredentials> credentials;
    private final ManifestCredentialRefreshDriver<WorkdayCredentials> refresh;
    private final ProviderHttpTransport transport;
    private final String baseUrl;
    private final int byteLimit;

    /**
     * Bind a configured API root, a caller-owned transport, and a positive response limit in bytes.
     */
    public WorkdayClient(
            CredentialProvider<WorkdayCredentials> credentials,
            String baseUrl,
            String tokenEndpoint,
            ProviderHttpTransport transport,
            int maxResponseBytes) {
        this.credentials = Objects.requireNonNull(credentials);
        this.transport = Objects.requireNonNull(transport);
        this.baseUrl = ProviderHttp.validateBaseUrl(baseUrl);
        if (maxResponseBytes <= 0)
            throw new IllegalArgumentException("Invalid Workday endpoint or response size");
        this.byteLimit = maxResponseBytes;
        this.refresh =
                new ManifestCredentialRefreshDriver<>(
                        "Workday",
                        AUTHORIZATION,
                        WorkdayCredentials.FIELDS,
                        transport,
                        Collections.singletonMap(
                                "tokenEndpoint", validateOAuthEndpoint(tokenEndpoint)),
                        Instant::now);
    }

    /** Load the connection's settings and bind its refresh-capable project credential provider. */
    public static WorkdayClient fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "workday".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Workday connection is missing"));
        if (!"workday".equals(selected.getProvider()))
            throw new IllegalArgumentException("Workday connection provider differs");
        String base = null;
        String tokenEndpoint = null;
        boolean hasAuthorizationEndpoint = false, hasScopes = false;
        long timeout = 15000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Workday API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "tokenEndpoint":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Workday token endpoint");
                    tokenEndpoint = value.textValue();
                    break;
                case "authorizationEndpoint":
                    if (!value.isTextual())
                        throw new IllegalArgumentException(
                                "Invalid Workday authorization endpoint");
                    validateOAuthEndpoint(value.textValue());
                    hasAuthorizationEndpoint = true;
                    break;
                case "scopes":
                    hasScopes = value.isArray() && value.size() > 0;
                    for (JsonNode scope : value) hasScopes &= scope.isTextual();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Workday timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Workday response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Workday configuration field");
            }
        }
        if (base == null || tokenEndpoint == null || !hasAuthorizationEndpoint || !hasScopes)
            throw new IllegalArgumentException("Required Workday tenant settings are missing");
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        return new WorkdayClient(
                WorkdayCredentials.fromProject(project, connectionName),
                base,
                tokenEndpoint,
                new ProviderHttpClient(Duration.ofNanos(timeout), policy),
                limit);
    }

    /** Bind the single-worker lookup. */
    public Query<GetWorkerInput, WorkerSummary> getWorker() {
        return new WorkersQueries.Get(this);
    }

    /** Bind the filtered page lookup. */
    public Query<ListWorkersInput, WorkerPage> listWorkers() {
        return new WorkersQueries.ListPage(this);
    }

    abstract static class WorkerQuery<I, O> implements Query<I, O> {
        final WorkdayClient client;
        final String operation;
        final O empty;
        private final QueryDefinition definition;

        WorkerQuery(WorkdayClient client, String operation, O empty) {
            this.client = client;
            this.operation = operation;
            this.empty = empty;
            try {
                definition =
                        JSON.treeToValue(DEFINITIONS.required(operation), QueryDefinition.class);
                definition.validate();
            } catch (IOException e) {
                throw new IllegalStateException("Invalid generated Workday operation", e);
            }
        }

        public final QueryDefinition definition() {
            return definition;
        }

        abstract String suffix(I input) throws IOException;

        abstract O decode(byte[] body, I input) throws IOException;

        abstract String successBranch();

        public final QueryAttempt<O> invoke(Call call, I input) throws InterruptedException {
            String path;
            try {
                path = suffix(input);
            } catch (IOException | IllegalArgumentException e) {
                return failed("defect", FailureKind.VALIDATION, "Invalid Workday query input");
            }
            String token;
            try {
                WorkdayCredentials resolved =
                        Credentials.resolveCredential(client.credentials, call, client.refresh);
                token = resolved == null ? "" : resolved.accessToken.reveal();
            } catch (ReauthorizationRequiredException e) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Workday connection needs authorization");
            } catch (IOException | RuntimeException e) {
                return retry(
                        FailureKind.TRANSPORT, "Workday credential refresh failed", Duration.ZERO);
            }
            if (!ProviderHttp.isHeaderSafeCredential(token))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Workday access token is unavailable");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", client.baseUrl + "/workers" + path)
                            .header("Authorization", "Bearer " + token)
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = client.transport.send(request)) {
                int status = response.getStatusCode();
                byte[] body = null;
                IOException readError = null;
                boolean oversized = false;
                try {
                    body = response.readBoundedBody(client.byteLimit);
                } catch (BodyTooLargeException e) {
                    oversized = true;
                } catch (IOException e) {
                    readError = e;
                }
                if (body != null && containsAccessToken(body, token))
                    return failed(
                            "defect",
                            FailureKind.AUTHENTICATION,
                            "Workday response contained a credential");
                if (status == 408 || status == 429 || status >= 500)
                    return retry(
                            status == 429 ? FailureKind.RATE_LIMIT : FailureKind.AVAILABILITY,
                            "Workday is temporarily unavailable",
                            ProviderHttp.parseRetryAfter(
                                    response.getHeaders().first("Retry-After"), Instant.now()));
                if (status == 404 && "getWorker".equals(operation))
                    return failed(
                            "notFound", FailureKind.NOT_FOUND, "Workday worker was not found");
                if (status != 200)
                    return failed(
                            "providerRejected",
                            FailureKind.PROVIDER_REJECTION,
                            "Workday rejected HTTP " + status);
                if (oversized)
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Workday response exceeded the configured limit");
                if (readError != null) throw readError;
                try {
                    return QueryAttempt.branch(
                            successBranch(), decode(body, input), Receipt.empty());
                } catch (IOException | IllegalArgumentException e) {
                    return failed(
                            "invalidResponse",
                            FailureKind.PROTOCOL,
                            "Workday returned invalid workers JSON");
                }
            } catch (IOException e) {
                return retry(FailureKind.TRANSPORT, "Workday request failed", Duration.ZERO);
            }
        }

        private QueryAttempt<O> failed(String branch, FailureKind kind, String message) {
            return QueryAttempt.branch(
                    branch,
                    empty,
                    Receipt.empty(),
                    new Failure(kind, "workday", operation, message));
        }

        private QueryAttempt<O> retry(FailureKind kind, String message, Duration delay) {
            return QueryAttempt.retry(new Failure(kind, "workday", operation, message), delay);
        }
    }

    private static boolean containsAccessToken(byte[] body, String token) {
        String wire = new String(body, java.nio.charset.StandardCharsets.UTF_8);
        if (wire.contains(token)) return true;
        if (wire.indexOf('\\') < 0) return false;
        try (com.fasterxml.jackson.core.JsonParser fields = JSON.getFactory().createParser(wire)) {
            com.fasterxml.jackson.core.JsonToken field;
            while ((field = fields.nextToken()) != null) {
                if ((field == com.fasterxml.jackson.core.JsonToken.FIELD_NAME
                                || field == com.fasterxml.jackson.core.JsonToken.VALUE_STRING)
                        && fields.getText().contains(token)) return true;
            }
        } catch (IOException ignored) {
            // The operation classifies malformed provider JSON.
        }
        return false;
    }

    private static String validateOAuthEndpoint(String endpoint) {
        try {
            URI uri = URI.create(endpoint);
            if (("https".equalsIgnoreCase(uri.getScheme())
                            || "http".equalsIgnoreCase(uri.getScheme()))
                    && uri.getHost() != null
                    && uri.getRawUserInfo() == null
                    && uri.getFragment() == null) return endpoint;
        } catch (IllegalArgumentException ignored) {
            // Omit the original URI from configuration errors.
        }
        throw new IllegalArgumentException("Invalid Workday OAuth endpoint");
    }

    private static JsonNode readResource(String name) {
        try (InputStream in =
                WorkdayClient.class.getResourceAsStream(
                        "/META-INF/dex-connectors/workday/" + name)) {
            return JSON.readTree(Objects.requireNonNull(in));
        } catch (IOException e) {
            throw new IllegalStateException("Invalid Workday resource", e);
        }
    }
}
