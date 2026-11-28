// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rainbond;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.net.URI;
import java.time.Duration;
import java.time.Instant;
import java.util.Iterator;
import java.util.Map;
import java.util.Objects;
import java.util.function.Function;

/** The Rainbond team query and its operation-specific Dex Step configuration. */
public final class TeamDirectory {
    private static final String CONNECTOR = "rainbond";
    private static final String OPERATION = "listTeams";
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

    private TeamDirectory() {}

    /** Select one page of teams. */
    public static final class ListTeamsInput {
        /** One-based page number up to 1,000,000. Zero selects the first page. */
        public final long page;

        /** Page size from 1 through 100. Zero selects 20. */
        public final long pageSize;

        /** Select the team list to request. */
        @JsonCreator
        public ListTeamsInput(
                @JsonProperty("page") long page, @JsonProperty("pageSize") long pageSize) {
            this.page = page;
            this.pageSize = pageSize;
        }
    }

    /** Concrete Dex Step input for a team result. */
    public static final class ListTeamsResult extends QueryResult<ListTeamsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListTeamsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListTeamsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the team query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListTeamsInput, ListTeamsOutput, ListTeamsResult>
                    newListTeamsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListTeamsInput, ListTeamsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListTeamsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Rainbond connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListTeamsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListTeamsOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "rainbond".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Rainbond connection is missing"));
        if (!"rainbond".equals(selected.getProvider()))
            throw new IllegalArgumentException("Rainbond connection provider differs");
        String base = "";
        long timeout = 10000000000L;
        int limit = 2 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Rainbond API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Rainbond timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Rainbond response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Rainbond configuration field");
            }
        }
        if (base.isEmpty())
            throw new IllegalArgumentException("Rainbond deployment origin is required");
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("rainbond", connectionName),
                        material -> {
                            JsonNode token = material.get("api_key");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Rainbond credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListTeamsOperation(
                provider, base, new ProviderHttpClient(Duration.ofNanos(timeout), policy), limit);
    }

    /** Reads one team list using bounded responses and a redirect-safe transport. */
    public static final class ListTeamsOperation implements Query<ListTeamsInput, ListTeamsOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the deployment origin and the timeout and byte limit from the manifest. */
        public ListTeamsOperation(CredentialProvider<SecretString> credentials, String baseUrl) {
            this(
                    credentials,
                    baseUrl,
                    new ProviderHttpClient(
                            Duration.ofSeconds(10),
                            java.net.URI.create(baseUrl).getScheme().equalsIgnoreCase("http")
                                    ? AddressPolicy.PRIVATE_NETWORK_HTTP
                                    : AddressPolicy.PUBLIC_HTTPS),
                    2097152);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public ListTeamsOperation(
                CredentialProvider<SecretString> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin = ProviderHttp.validateBaseUrl(baseUrl);
            if (!java.net.URI.create(origin).getPath().isEmpty())
                throw new IllegalArgumentException(
                        "Rainbond requires a deployment origin without a path");
            this.endpoint = origin;
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Rainbond response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListTeamsOutput> invoke(Call call, ListTeamsInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid Rainbond API token");
            long pageNumber = input.page == 0 ? 1 : input.page;
            long pageSize = input.pageSize == 0 ? 20 : input.pageSize;
            if (pageNumber < 1 || pageNumber > 1000000 || pageSize < 1 || pageSize > 100)
                return failed("defect", FailureKind.VALIDATION, "Invalid team list number or size");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET",
                                    endpoint
                                            + "/openapi/v1/teams"
                                            + "?page="
                                            + pageNumber
                                            + "&page_size="
                                            + pageSize)
                            .header("Accept", "application/json")
                            .header("Authorization", key.reveal())
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status == 429 || status == 408 || status >= 500)
                    return classifyStatus(response);
                byte[] body = response.readBoundedBody(maxResponseBytes);
                if (status != 200)
                    return failed(
                            "providerRejected",
                            status == 401
                                    ? FailureKind.AUTHENTICATION
                                    : status == 403
                                            ? FailureKind.AUTHORIZATION
                                            : FailureKind.PROVIDER_REJECTION,
                            "Rainbond rejected the request");
                try {
                    return decodeTeams(body, pageSize);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Rainbond response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Rainbond request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/rainbond/operation-definitions.json";
        try (InputStream input = TeamDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListTeamsOutput> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(
                    FailureKind.RATE_LIMIT, "Rainbond rate limited the project request", delay);
        return retry(FailureKind.AVAILABILITY, "Rainbond is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 512
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public Long total;
        public WireItem[] tenants;
    }

    private static final class WireItem {
        public String tenant_id;
        public String tenant_name;
        public Boolean is_active;
    }

    private static int length(String value) {
        return value.getBytes(java.nio.charset.StandardCharsets.UTF_8).length;
    }

    private static boolean hasEdgeSpace(String key) {
        return isProviderSpace(key.codePointAt(0))
                || isProviderSpace(key.codePointBefore(key.length()));
    }

    private static boolean isProviderSpace(int codePoint) {
        return (codePoint >= 9 && codePoint <= 13)
                || codePoint == 0x85
                || Character.isSpaceChar(codePoint);
    }

    private static QueryAttempt<ListTeamsOutput> decodeTeams(byte[] body, long pageSize)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null) return invalid();
        WireItem[] items = page.tenants;
        if (items == null || page.total == null || page.total < 0 || items.length > pageSize)
            return invalid();
        java.util.List<ListTeamsOutput.Team> values = new java.util.ArrayList<>(items.length);
        for (WireItem item : items) {
            if (item == null
                    || item.tenant_id == null
                    || item.tenant_id.isEmpty()
                    || hasEdgeSpace(item.tenant_id)
                    || length(item.tenant_id) > 256
                    || item.tenant_name == null
                    || item.tenant_name.isEmpty()
                    || length(item.tenant_name) > 512) return invalid();
            values.add(
                    new ListTeamsOutput.Team(
                            item.tenant_id, item.tenant_name, Boolean.TRUE.equals(item.is_active)));
        }
        return QueryAttempt.branch(
                "listed", new ListTeamsOutput(values, page.total), Receipt.empty());
    }

    private static QueryAttempt<ListTeamsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid team response");
    }

    private static QueryAttempt<ListTeamsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListTeamsOutput(java.util.Collections.emptyList(), 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListTeamsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
