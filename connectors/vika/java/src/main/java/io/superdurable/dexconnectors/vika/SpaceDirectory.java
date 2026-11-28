// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.vika;

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

/** The Vika space query and its operation-specific Dex Step configuration. */
public final class SpaceDirectory {
    private static final String CONNECTOR = "vika";
    private static final String OPERATION = "listSpaces";
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

    private SpaceDirectory() {}

    /** Empty input for the listSpaces query. */
    public static final class ListSpacesInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Concrete Dex Step input for a space result. */
    public static final class ListSpacesResult extends QueryResult<SpacePage> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListSpacesResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") SpacePage value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the space query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListSpacesInput, SpacePage, ListSpacesResult>
                    newListSpacesStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListSpacesInput, SpacePage> operation,
                            ConnectionRef connection,
                            Function<I, ListSpacesInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Vika connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListSpacesResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListSpacesOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "vika".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("Vika connection is missing"));
        if (!"vika".equals(selected.getProvider()))
            throw new IllegalArgumentException("Vika connection provider differs");
        String base = "https://vika.cn";
        long timeout = 15000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Vika API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Vika timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Vika response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Vika configuration field");
            }
        }
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("vika", connectionName),
                        material -> {
                            JsonNode token = material.get("api_token");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Vika credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListSpacesOperation(
                provider, base, new ProviderHttpClient(Duration.ofNanos(timeout), policy), limit);
    }

    /** Reads one space list using bounded responses and a redirect-safe transport. */
    public static final class ListSpacesOperation implements Query<ListSpacesInput, SpacePage> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the deployment origin and the timeout and byte limit from the manifest. */
        public ListSpacesOperation(CredentialProvider<SecretString> credentials, String baseUrl) {
            this(
                    credentials,
                    baseUrl,
                    new ProviderHttpClient(
                            Duration.ofSeconds(15),
                            baseUrl != null
                                            && "http"
                                                    .equalsIgnoreCase(
                                                            java.net
                                                                    .URI
                                                                    .create(baseUrl)
                                                                    .getScheme())
                                    ? AddressPolicy.PRIVATE_NETWORK_HTTP
                                    : AddressPolicy.PUBLIC_HTTPS),
                    1048576);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public ListSpacesOperation(
                CredentialProvider<SecretString> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? "https://vika.cn" : baseUrl);
            this.endpoint = origin;
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Vika response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<SpacePage> invoke(Call call, ListSpacesInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid Vika API token");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", endpoint + "/fusion/v1/spaces")
                            .header("Accept", "application/json")
                            .header("Authorization", "Bearer " + key.reveal())
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
                                            : (status == 400 || status == 404)
                                                    ? FailureKind.VALIDATION
                                                    : FailureKind.PROVIDER_REJECTION,
                            "Vika rejected the request");
                try {
                    return decodeSpaces(body, response);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Vika response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Vika request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/vika/operation-definitions.json";
        try (InputStream input = SpaceDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<SpacePage> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Vika rate limited the project request", delay);
        return retry(FailureKind.AVAILABILITY, "Vika is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 512
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public Boolean success;
        public Long code;
        public DirectoryData data;
    }

    private static final class DirectoryData {
        public WireItem[] spaces;
    }

    private static final class WireItem {
        public String id;
        public String name;
        public Boolean isAdmin;
    }

    private static QueryAttempt<SpacePage> decodeSpaces(byte[] body, ProviderHttpResponse response)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null) return invalid();
        if (page.success == null || page.code == null) return invalid();
        if (!page.success) {
            if (page.code == 429 || page.code >= 500)
                return retry(
                        page.code == 429 ? FailureKind.RATE_LIMIT : FailureKind.AVAILABILITY,
                        "Vika is temporarily unavailable",
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now()));
            if (page.code == 200)
                return QueryAttempt.branch(
                        "listed",
                        new SpacePage(java.util.Collections.emptyList()),
                        Receipt.empty());
            return failed(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "Vika rejected the query");
        }
        if (page.code != 200) return invalid();
        WireItem[] items = page.data == null ? null : page.data.spaces;
        if (items == null || items.length > 1000) return invalid();
        java.util.List<SpacePage.Space> values = new java.util.ArrayList<>(items.length);
        for (WireItem item : items) {
            if (item == null
                    || item.id == null
                    || item.id.isEmpty()
                    || !item.id.startsWith("spc")
                    || item.name == null
                    || item.name.isEmpty()) return invalid();
            values.add(new SpacePage.Space(item.id, item.name, Boolean.TRUE.equals(item.isAdmin)));
        }
        return QueryAttempt.branch("listed", new SpacePage(values), Receipt.empty());
    }

    private static QueryAttempt<SpacePage> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid space response");
    }

    private static QueryAttempt<SpacePage> failed(String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new SpacePage(java.util.Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<SpacePage> retry(FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
