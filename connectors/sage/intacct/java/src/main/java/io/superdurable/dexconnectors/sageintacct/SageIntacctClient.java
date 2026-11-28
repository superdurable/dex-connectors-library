// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.oauthtoken.ManifestCredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.*;
import java.net.URI;
import java.time.*;
import java.util.*;

/** Shared binding for bounded Sage Intacct invoice reads and renewable OAuth credentials. */
public final class SageIntacctClient {
    static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final JsonNode DEFINITIONS = readResource("operation-definitions.json");
    private static final JsonNode AUTHORIZATION = readResource("refresh-authorization.json");
    private final CredentialProvider<SageIntacctCredentials> credentials;
    private final ManifestCredentialRefreshDriver<SageIntacctCredentials> refresh;
    private final ProviderHttpTransport transport;
    private final String baseUrl;
    private final int byteLimit;

    /**
     * Bind a configured API root, a caller-owned transport, and a positive response limit in bytes.
     */
    public SageIntacctClient(
            CredentialProvider<SageIntacctCredentials> credentials,
            String baseUrl,
            ProviderHttpTransport transport,
            int maxResponseBytes) {
        this.credentials = Objects.requireNonNull(credentials);
        this.transport = Objects.requireNonNull(transport);
        this.baseUrl = ProviderHttp.validateBaseUrl(baseUrl);
        if (maxResponseBytes <= 0)
            throw new IllegalArgumentException("Invalid SageIntacct endpoint or response size");
        this.byteLimit = maxResponseBytes;
        this.refresh =
                new ManifestCredentialRefreshDriver<>(
                        "SageIntacct",
                        AUTHORIZATION,
                        SageIntacctCredentials.FIELDS,
                        transport,
                        Collections.emptyMap(),
                        Instant::now);
    }

    /** Load the connection's settings and bind its refresh-capable project credential provider. */
    public static SageIntacctClient fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "sage-intacct".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("SageIntacct connection is missing"));
        if (!"sage-intacct".equals(selected.getProvider()))
            throw new IllegalArgumentException("SageIntacct connection provider differs");
        String base = "https://api.intacct.com";
        long timeout = 15000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid SageIntacct API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid SageIntacct timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid SageIntacct response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown SageIntacct configuration field");
            }
        }
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        return new SageIntacctClient(
                SageIntacctCredentials.fromProject(project, connectionName),
                base,
                new ProviderHttpClient(Duration.ofNanos(timeout), policy),
                limit);
    }

    /** Bind the single-invoice lookup. */
    public Query<GetInvoiceInput, InvoiceSummary> getInvoice() {
        return new InvoiceQueries.Get(this);
    }

    /** Bind the first invoice page lookup. */
    public Query<ListInvoicesInput, InvoiceList> listInvoices() {
        return new InvoiceQueries.ListPage(this);
    }

    abstract static class InvoiceQuery<I, O> implements Query<I, O> {
        final SageIntacctClient client;
        final String operation;
        final O empty;
        private final QueryDefinition definition;

        InvoiceQuery(SageIntacctClient client, String operation, O empty) {
            this.client = client;
            this.operation = operation;
            this.empty = empty;
            try {
                definition =
                        JSON.treeToValue(DEFINITIONS.required(operation), QueryDefinition.class);
                definition.validate();
            } catch (IOException e) {
                throw new IllegalStateException("Invalid generated SageIntacct operation", e);
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
                return failed("defect", FailureKind.VALIDATION, "Invalid Sage Intacct query input");
            }
            String token;
            try {
                SageIntacctCredentials resolved =
                        Credentials.resolveCredential(client.credentials, call, client.refresh);
                token = resolved == null ? "" : resolved.accessToken.reveal();
            } catch (ReauthorizationRequiredException e) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "SageIntacct connection needs authorization");
            } catch (IOException | RuntimeException e) {
                return retry(
                        FailureKind.TRANSPORT,
                        "SageIntacct credential refresh failed",
                        Duration.ZERO);
            }
            if (!ProviderHttp.isHeaderSafeCredential(token))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "SageIntacct access token is unavailable");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET",
                                    client.baseUrl
                                            + "/ia/api/v1/objects/accounts-receivable/invoice"
                                            + path)
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
                            "SageIntacct response contained a credential");
                if (status == 408 || status == 429 || status >= 500)
                    return retry(
                            status == 429 ? FailureKind.RATE_LIMIT : FailureKind.AVAILABILITY,
                            "SageIntacct is temporarily unavailable",
                            ProviderHttp.parseRetryAfter(
                                    response.getHeaders().first("Retry-After"), Instant.now()));
                if (status == 404 && "getInvoice".equals(operation))
                    return failed(
                            "notFound", FailureKind.NOT_FOUND, "SageIntacct invoice was not found");
                if (status != 200)
                    return failed(
                            "providerRejected",
                            FailureKind.PROVIDER_REJECTION,
                            "SageIntacct rejected HTTP " + status);
                if (oversized)
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "SageIntacct response exceeded the configured limit");
                if (readError != null) throw readError;
                try {
                    return QueryAttempt.branch(
                            successBranch(), decode(body, input), Receipt.empty());
                } catch (IOException | IllegalArgumentException e) {
                    return failed(
                            "invalidResponse",
                            FailureKind.PROTOCOL,
                            "SageIntacct returned invalid invoice JSON");
                }
            } catch (IOException e) {
                return retry(FailureKind.TRANSPORT, "SageIntacct request failed", Duration.ZERO);
            }
        }

        private QueryAttempt<O> failed(String branch, FailureKind kind, String message) {
            return QueryAttempt.branch(
                    branch,
                    empty,
                    Receipt.empty(),
                    new Failure(kind, "sage-intacct", operation, message));
        }

        private QueryAttempt<O> retry(FailureKind kind, String message, Duration delay) {
            return QueryAttempt.retry(new Failure(kind, "sage-intacct", operation, message), delay);
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

    private static JsonNode readResource(String name) {
        try (InputStream in =
                SageIntacctClient.class.getResourceAsStream(
                        "/META-INF/dex-connectors/sage/intacct/" + name)) {
            return JSON.readTree(Objects.requireNonNull(in));
        } catch (IOException e) {
            throw new IllegalStateException("Invalid SageIntacct resource", e);
        }
    }
}
