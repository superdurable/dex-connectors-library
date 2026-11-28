// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qingflow;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.time.Duration;
import java.time.Instant;
import java.util.Iterator;
import java.util.Map;
import java.util.Objects;
import java.util.function.Function;

/** The Qingflow application query and its operation-specific Dex Step configuration. */
public final class ApplicationDirectory {
    private static final String CONNECTOR = "qingflow";
    private static final String OPERATION = "listApplications";
    private static final ObjectMapper JSON =
            new ObjectMapper()
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
                    .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
                    .disable(
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT,
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);

    static {
        com.fasterxml.jackson.databind.cfg.MutableCoercionConfig strings =
                JSON.coercionConfigFor(com.fasterxml.jackson.databind.type.LogicalType.Textual);
        for (com.fasterxml.jackson.databind.cfg.CoercionInputShape shape :
                new com.fasterxml.jackson.databind.cfg.CoercionInputShape[] {
                    com.fasterxml.jackson.databind.cfg.CoercionInputShape.Integer,
                    com.fasterxml.jackson.databind.cfg.CoercionInputShape.Float,
                    com.fasterxml.jackson.databind.cfg.CoercionInputShape.Boolean
                }) {
            strings.setCoercion(shape, com.fasterxml.jackson.databind.cfg.CoercionAction.Fail);
        }
    }

    private static final QueryDefinition DEFINITION = loadDefinition();

    private ApplicationDirectory() {}

    /** Empty input for the listApplications query. */
    public static final class ListApplicationsInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Concrete Dex Step input for a application result. */
    public static final class ListApplicationsResult extends QueryResult<ListApplicationsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListApplicationsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListApplicationsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the application query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            ListApplicationsInput,
                            ListApplicationsOutput,
                            ListApplicationsResult>
                    newListApplicationsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListApplicationsInput, ListApplicationsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListApplicationsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Qingflow connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListApplicationsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListApplicationsOperation fromProject(
            LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "qingflow".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Qingflow connection is missing"));
        if (!"qingflow".equals(selected.getProvider()))
            throw new IllegalArgumentException("Qingflow connection provider differs");
        long timeout = 10000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Qingflow timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Qingflow response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Qingflow configuration field");
            }
        }
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("qingflow", connectionName),
                        material -> {
                            JsonNode token = material.get("access_token");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Qingflow credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListApplicationsOperation(
                provider,
                new ProviderHttpClient(Duration.ofNanos(timeout), AddressPolicy.PUBLIC_HTTPS),
                limit);
    }

    /** Reads one application list using bounded responses and a redirect-safe transport. */
    public static final class ListApplicationsOperation
            implements Query<ListApplicationsInput, ListApplicationsOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private static final String ENDPOINT = "https://api.qingflow.com/app";
        private final int maxResponseBytes;

        /** Use the fixed provider endpoint with the manifest timeout and response limit. */
        public ListApplicationsOperation(CredentialProvider<SecretString> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.PUBLIC_HTTPS),
                    1 << 20);
        }

        /**
         * Bind the credential provider, caller-owned transport and maximum response size in bytes.
         */
        public ListApplicationsOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > (16 << 20)) {
                throw new IllegalArgumentException("Qingflow response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListApplicationsOutput> invoke(Call call, ListApplicationsInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid Qingflow API token");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", ENDPOINT)
                            .header("Accept", "application/json")
                            .header("Accesstoken", key.reveal())
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status == 429 || status == 408 || status >= 500)
                    return classifyStatus(response);
                if (status != 200)
                    return failed(
                            "providerRejected",
                            status == 401
                                    ? FailureKind.AUTHENTICATION
                                    : status == 403
                                            ? FailureKind.AUTHORIZATION
                                            : FailureKind.PROVIDER_REJECTION,
                            "Qingflow rejected the request");
                byte[] body = response.readBoundedBody(maxResponseBytes);
                try {
                    return decodeApplications(body);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Qingflow response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Qingflow request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/qingflow/operation-definitions.json";
        try (InputStream input = ApplicationDirectory.class.getResourceAsStream(resource)) {
            QueryDefinition definition =
                    JSON.treeToValue(
                            JSON.readTree(Objects.requireNonNull(input)).get(OPERATION),
                            QueryDefinition.class);
            definition.validate();
            return definition;
        } catch (IOException error) {
            throw new ExceptionInInitializerError(error);
        }
    }

    private static QueryAttempt<ListApplicationsOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(
                    FailureKind.RATE_LIMIT, "Qingflow rate limited the project request", delay);
        return retry(FailureKind.AVAILABILITY, "Qingflow is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 1024
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public Long errCode;
        public DirectoryData result;
    }

    private static final class DirectoryData {
        public WireItem[] appList;
    }

    private static final class WireItem {
        public String appKey;
        public String appName;
    }

    private static QueryAttempt<ListApplicationsOutput> decodeApplications(byte[] body)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null) return invalid();
        if (page.errCode == null) return invalid();
        if (page.errCode != 0)
            return failed(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "Qingflow rejected the query");
        WireItem[] items = page.result == null ? null : page.result.appList;
        if (items == null) return invalid();
        java.util.List<ListApplicationsOutput.ApplicationMetadata> values =
                new java.util.ArrayList<>(items.length);
        for (WireItem item : items) {
            if (item == null
                    || item.appKey == null
                    || item.appKey.isEmpty()
                    || item.appName == null
                    || item.appName.isEmpty()) return invalid();
            values.add(new ListApplicationsOutput.ApplicationMetadata(item.appKey, item.appName));
        }
        return QueryAttempt.branch("listed", new ListApplicationsOutput(values), Receipt.empty());
    }

    private static QueryAttempt<ListApplicationsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid application response");
    }

    private static QueryAttempt<ListApplicationsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListApplicationsOutput(java.util.Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListApplicationsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
