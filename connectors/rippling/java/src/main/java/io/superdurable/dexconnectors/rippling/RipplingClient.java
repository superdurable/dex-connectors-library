// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.*;
import java.net.URI;
import java.time.*;
import java.util.*;

/** Shared binding for bounded Rippling directory reads and API token credentials. */
public final class RipplingClient {
    static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final JsonNode DEFINITIONS = readResource("operation-definitions.json");

    private final CredentialProvider<SecretString> credentials;

    private final ProviderHttpTransport transport;
    private final String baseUrl;
    private final int byteLimit;

    /**
     * Bind a configured API root, a caller-owned transport, and a positive response limit in bytes.
     */
    public RipplingClient(
            CredentialProvider<SecretString> credentials,
            String baseUrl,
            ProviderHttpTransport transport,
            int maxResponseBytes) {
        this.credentials = Objects.requireNonNull(credentials);
        this.transport = Objects.requireNonNull(transport);
        this.baseUrl = ProviderHttp.validateBaseUrl(baseUrl);
        if (maxResponseBytes <= 0)
            throw new IllegalArgumentException("Invalid Rippling endpoint or response size");
        this.byteLimit = maxResponseBytes;
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static RipplingClient fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "rippling".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Rippling connection is missing"));
        if (!"rippling".equals(selected.getProvider()))
            throw new IllegalArgumentException("Rippling connection provider differs");
        String base = "https://rest.ripplingapis.com";
        long timeout = 15000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Rippling API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Rippling timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Rippling response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Rippling configuration field");
            }
        }
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("rippling", connectionName),
                        material -> {
                            JsonNode token = material.get("api_token");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Rippling credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new RipplingClient(
                provider, base, new ProviderHttpClient(Duration.ofNanos(timeout), policy), limit);
    }

    /** Bind the single-worker lookup. */
    public Query<GetWorkerInput, WorkerSummary> getWorker() {
        return new WorkersQueries.Get(this);
    }

    /** Bind the worker page lookup. */
    public Query<ListWorkersInput, WorkerPage> listWorkers() {
        return new WorkersQueries.ListPage(this);
    }

    abstract static class WorkerQuery<I, O> implements Query<I, O> {
        final RipplingClient client;
        final String operation;
        final O empty;
        private final QueryDefinition definition;

        WorkerQuery(RipplingClient client, String operation, O empty) {
            this.client = client;
            this.operation = operation;
            this.empty = empty;
            try {
                definition =
                        JSON.treeToValue(DEFINITIONS.required(operation), QueryDefinition.class);
                definition.validate();
            } catch (IOException e) {
                throw new IllegalStateException("Invalid generated Rippling operation", e);
            }
        }

        public final QueryDefinition definition() {
            return definition;
        }

        abstract String suffix(I input) throws IOException;

        abstract O decode(byte[] body, I input) throws IOException;

        abstract boolean containsCredential(O output, String token);

        public final QueryAttempt<O> invoke(Call call, I input) throws InterruptedException {
            String path;
            try {
                path = suffix(input);
            } catch (IOException | IllegalArgumentException e) {
                return failed("defect", FailureKind.VALIDATION, "Invalid Rippling query input");
            }
            String token;
            try {
                SecretString resolved = client.credentials.resolve(call);
                token = resolved == null ? "" : resolved.reveal();
            } catch (IOException | RuntimeException e) {
                return failed(
                        "defect", FailureKind.AUTHENTICATION, "Rippling API token is unavailable");
            }
            if (!ProviderHttp.isHeaderSafeCredential(token))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Rippling access token is unavailable");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", client.baseUrl + "/workers/" + path)
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
                if (status == 408 || status == 429 || status >= 500)
                    return retry(
                            status == 429 ? FailureKind.RATE_LIMIT : FailureKind.AVAILABILITY,
                            "Rippling is temporarily unavailable",
                            ProviderHttp.parseRetryAfter(
                                    response.getHeaders().first("Retry-After"), Instant.now()));
                if (status == 404 && "getWorker".equals(operation))
                    return failed(
                            "notFound", FailureKind.NOT_FOUND, "Rippling worker was not found");
                if (status != 200)
                    return failed(
                            "providerRejected",
                            FailureKind.PROVIDER_REJECTION,
                            "Rippling rejected HTTP " + status);
                if (oversized)
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Rippling response exceeded the configured limit");
                if (readError != null) throw readError;
                try {
                    O output = decode(body, input);
                    if (containsCredential(output, token))
                        return failed(
                                "defect",
                                FailureKind.AUTHENTICATION,
                                "Rippling response included a credential");
                    return QueryAttempt.branch("found", output, Receipt.empty());
                } catch (IOException | IllegalArgumentException e) {
                    return failed(
                            "invalidResponse",
                            FailureKind.PROTOCOL,
                            "Rippling returned invalid workers JSON");
                }
            } catch (IOException e) {
                return retry(FailureKind.TRANSPORT, "Rippling request failed", Duration.ZERO);
            }
        }

        private QueryAttempt<O> failed(String branch, FailureKind kind, String message) {
            return QueryAttempt.branch(
                    branch,
                    empty,
                    Receipt.empty(),
                    new Failure(kind, "rippling", operation, message));
        }

        private QueryAttempt<O> retry(FailureKind kind, String message, Duration delay) {
            return QueryAttempt.retry(new Failure(kind, "rippling", operation, message), delay);
        }
    }

    private static JsonNode readResource(String name) {
        try (InputStream in =
                RipplingClient.class.getResourceAsStream(
                        "/META-INF/dex-connectors/rippling/" + name)) {
            return JSON.readTree(Objects.requireNonNull(in));
        } catch (IOException e) {
            throw new IllegalStateException("Invalid Rippling resource", e);
        }
    }
}
