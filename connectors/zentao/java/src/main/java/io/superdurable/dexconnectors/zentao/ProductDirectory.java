// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zentao;

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

/** The ZenTao product query and its operation-specific Dex Step configuration. */
public final class ProductDirectory {
    private static final String CONNECTOR = "zentao";
    private static final String OPERATION = "listProducts";
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

    private ProductDirectory() {}

    /** Empty input for the listProducts query. */
    public static final class ListProductsInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Concrete Dex Step input for a product result. */
    public static final class ListProductsResult extends QueryResult<ProductList> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListProductsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ProductList value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the product query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListProductsInput, ProductList, ListProductsResult>
                    newListProductsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListProductsInput, ProductList> operation,
                            ConnectionRef connection,
                            Function<I, ListProductsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("ZenTao connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListProductsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListProductsOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "zentao".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("ZenTao connection is missing"));
        if (!"zentao".equals(selected.getProvider()))
            throw new IllegalArgumentException("ZenTao connection provider differs");
        String base = "";
        long timeout = 15000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid ZenTao API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid ZenTao timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid ZenTao response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown ZenTao configuration field");
            }
        }
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("zentao", connectionName),
                        material -> {
                            JsonNode token = material.get("token");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid ZenTao credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListProductsOperation(
                provider, base, new ProviderHttpClient(Duration.ofNanos(timeout), policy), limit);
    }

    /** Reads one product list using bounded responses and a redirect-safe transport. */
    public static final class ListProductsOperation
            implements Query<ListProductsInput, ProductList> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the deployment origin and the timeout and byte limit from the manifest. */
        public ListProductsOperation(CredentialProvider<SecretString> credentials, String baseUrl) {
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
        public ListProductsOperation(
                CredentialProvider<SecretString> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin = ProviderHttp.validateBaseUrl(Objects.requireNonNull(baseUrl));
            if (origin.endsWith("/api.php") || origin.contains("/api.php/"))
                throw new IllegalArgumentException(
                        "ZenTao requires the installation root without /api.php");
            this.endpoint = origin;
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("ZenTao response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ProductList> invoke(Call call, ListProductsInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid ZenTao API token");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", endpoint + "/api.php/v1/products")
                            .header("Accept", "application/json")
                            .header("Token", key.reveal())
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
                            "ZenTao rejected the request");
                try {
                    return decodeProducts(body);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "ZenTao response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "ZenTao request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/zentao/operation-definitions.json";
        try (InputStream input = ProductDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ProductList> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "ZenTao rate limited the request", delay);
        return retry(FailureKind.AVAILABILITY, "ZenTao is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0 && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public Long total;
        public WireItem[] products;
    }

    private static final class WireItem {
        public Long id;
        public String name;
        public String code;
        public String type;
        public String acl;
    }

    private static QueryAttempt<ProductList> decodeProducts(byte[] body) throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null) return invalid();
        WireItem[] items = page.products;
        if (items == null || page.total == null || page.total < 0 || items.length > 1000)
            return invalid();
        java.util.List<ProductList.Product> values = new java.util.ArrayList<>();
        for (WireItem item : items) {
            if (item == null) return invalid();
            if (item.id == null
                    || item.id <= 0
                    || item.name == null
                    || item.name.isEmpty()
                    || !("normal".equals(item.type)
                            || "branch".equals(item.type)
                            || "platform".equals(item.type))
                    || !("open".equals(item.acl) || "private".equals(item.acl))) return invalid();
            if (item.code == null) item.code = "";
            values.add(new ProductList.Product(item.id, item.name, item.code, item.type, item.acl));
        }
        return QueryAttempt.branch("listed", new ProductList(values, page.total), Receipt.empty());
    }

    private static QueryAttempt<ProductList> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid product response");
    }

    private static QueryAttempt<ProductList> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ProductList(java.util.Collections.emptyList(), 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ProductList> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
