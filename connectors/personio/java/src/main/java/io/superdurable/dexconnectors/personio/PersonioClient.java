// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.oauthtoken.ManifestCredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.*;
import java.net.URI;
import java.time.*;
import java.util.*;

/** Shared binding for bounded Personio v2 API reads and renewable client credentials. */
public final class PersonioClient {
    static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final String BASE_URL = "https://api.personio.de/v2";
    private static final JsonNode DEFINITIONS = readResource("operation-definitions.json");
    private static final JsonNode AUTHORIZATION = readResource("refresh-authorization.json");
    private final CredentialProvider<PersonioCredentials> credentials;
    private final ManifestCredentialRefreshDriver<PersonioCredentials> refresh;
    private final ProviderHttpTransport transport;
    private final String baseUrl;
    private final int byteLimit;

    /** Use the public API, a fifteen-second request timeout, and a 1 MiB response limit. */
    public PersonioClient(CredentialProvider<PersonioCredentials> credentials) {
        this(
                credentials,
                BASE_URL,
                new ProviderHttpClient(Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                1 << 20);
    }

    /**
     * Bind a configured API root, a caller-owned transport, and a positive response limit in bytes.
     */
    public PersonioClient(
            CredentialProvider<PersonioCredentials> credentials,
            String baseUrl,
            ProviderHttpTransport transport,
            int maxResponseBytes) {
        this.credentials = Objects.requireNonNull(credentials);
        this.transport = Objects.requireNonNull(transport);
        this.baseUrl = ProviderHttp.validateBaseUrl(baseUrl);
        if (!"/v2".equals(URI.create(this.baseUrl).getRawPath()) || maxResponseBytes <= 0)
            throw new IllegalArgumentException("Invalid Personio endpoint or response size");
        this.byteLimit = maxResponseBytes;
        this.refresh =
                new ManifestCredentialRefreshDriver<>(
                        "Personio", AUTHORIZATION, PersonioCredentials.FIELDS, transport);
    }

    /** Load the connection's settings and bind its refresh-capable project credential provider. */
    public static PersonioClient fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "personio".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Personio connection is missing"));
        if (!"personio".equals(selected.getProvider()))
            throw new IllegalArgumentException("Personio connection provider differs");
        String base = BASE_URL;
        long timeout = 15000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Personio API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Personio timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Personio response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Personio configuration field");
            }
        }
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        return new PersonioClient(
                PersonioCredentials.fromProject(project, connectionName),
                base,
                new ProviderHttpClient(Duration.ofNanos(timeout), policy),
                limit);
    }

    /** Bind the single-person lookup. */
    public Query<GetPersonInput, PersonProfile> getPerson() {
        return new PersonsQueries.Get(this);
    }

    /** Bind the filtered page lookup. */
    public Query<ListPersonsInput, PersonPage> listPersons() {
        return new PersonsQueries.ListPage(this);
    }

    abstract static class PersonQuery<I, O> implements Query<I, O> {
        final PersonioClient client;
        final String operation;
        final O empty;
        private final QueryDefinition definition;

        PersonQuery(PersonioClient client, String operation, O empty) {
            this.client = client;
            this.operation = operation;
            this.empty = empty;
            try {
                definition =
                        JSON.treeToValue(DEFINITIONS.required(operation), QueryDefinition.class);
                definition.validate();
            } catch (IOException e) {
                throw new IllegalStateException("Invalid generated Personio operation", e);
            }
        }

        public final QueryDefinition definition() {
            return definition;
        }

        abstract String suffix(I input) throws IOException;

        abstract O decode(byte[] body, I input, String baseUrl) throws IOException;

        abstract String successBranch();

        public final QueryAttempt<O> invoke(Call call, I input) throws InterruptedException {
            String path;
            try {
                path = suffix(input);
            } catch (IOException | IllegalArgumentException e) {
                return failed("defect", FailureKind.VALIDATION, "Invalid Personio query input");
            }
            String token;
            try {
                PersonioCredentials resolved =
                        Credentials.resolveCredential(client.credentials, call, client.refresh);
                token = resolved == null ? "" : resolved.accessToken.reveal();
            } catch (ReauthorizationRequiredException e) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Personio connection needs authorization");
            } catch (IOException | RuntimeException e) {
                return retry(
                        FailureKind.TRANSPORT, "Personio credential refresh failed", Duration.ZERO);
            }
            if (!ProviderHttp.isHeaderSafeCredential(token))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Personio access token is unavailable");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", client.baseUrl + "/persons" + path)
                            .header("Authorization", "Bearer " + token)
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = client.transport.send(request)) {
                int status = response.getStatusCode();
                if (status == 408 || status == 429 || status >= 500)
                    return retry(
                            status == 429 ? FailureKind.RATE_LIMIT : FailureKind.AVAILABILITY,
                            "Personio is temporarily unavailable",
                            ProviderHttp.parseRetryAfter(
                                    response.getHeaders().first("Retry-After"), Instant.now()));
                if (status == 404 && "getPerson".equals(operation))
                    return failed(
                            "notFound", FailureKind.NOT_FOUND, "Personio person was not found");
                if (status != 200)
                    return failed(
                            "providerRejected",
                            FailureKind.PROVIDER_REJECTION,
                            "Personio rejected HTTP " + status);
                byte[] body;
                try {
                    body = response.readBoundedBody(client.byteLimit);
                } catch (BodyTooLargeException e) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Personio response exceeded the configured limit");
                }
                try {
                    return QueryAttempt.branch(
                            successBranch(), decode(body, input, client.baseUrl), Receipt.empty());
                } catch (IOException | IllegalArgumentException e) {
                    return failed(
                            "invalidResponse",
                            FailureKind.PROTOCOL,
                            "Personio returned invalid persons JSON");
                }
            } catch (IOException e) {
                return retry(FailureKind.TRANSPORT, "Personio request failed", Duration.ZERO);
            }
        }

        private QueryAttempt<O> failed(String branch, FailureKind kind, String message) {
            return QueryAttempt.branch(
                    branch,
                    empty,
                    Receipt.empty(),
                    new Failure(kind, "personio", operation, message));
        }

        private QueryAttempt<O> retry(FailureKind kind, String message, Duration delay) {
            return QueryAttempt.retry(new Failure(kind, "personio", operation, message), delay);
        }
    }

    private static JsonNode readResource(String name) {
        try (InputStream in =
                PersonioClient.class.getResourceAsStream(
                        "/META-INF/dex-connectors/personio/" + name)) {
            return JSON.readTree(Objects.requireNonNull(in));
        } catch (IOException e) {
            throw new IllegalStateException("Invalid Personio resource", e);
        }
    }
}
