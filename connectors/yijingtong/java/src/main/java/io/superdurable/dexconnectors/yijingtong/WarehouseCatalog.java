// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yijingtong;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.time.Duration;
import java.time.Instant;
import java.util.Objects;
import java.util.function.Function;

/** The Yijingtong warehouse query and its operation-specific Dex Step configuration. */
public final class WarehouseCatalog {
    private static final String CONNECTOR = "yijingtong";
    private static final String OPERATION = "listWarehouses";
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

    private WarehouseCatalog() {}

    /** The warehouse query takes an empty JSON object. */
    public static final class ListWarehousesInput {}

    /** Concrete Dex Step input for a warehouse result. */
    public static final class ListWarehousesResult extends QueryResult<WarehousePage> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListWarehousesResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") WarehousePage value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the warehouse query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListWarehousesInput, WarehousePage, ListWarehousesResult>
                    newListWarehousesStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListWarehousesInput, WarehousePage> operation,
                            ConnectionRef connection,
                            Function<I, ListWarehousesInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Yijingtong connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListWarehousesResult::new);
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class ListWarehousesOperation
            implements Query<ListWarehousesInput, WarehousePage> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListWarehousesOperation(
                CredentialProvider<SecretString> credentials, String baseUrl) {
            this(
                    credentials,
                    baseUrl,
                    new ProviderHttpClient(
                            Duration.ofSeconds(15),
                            ProviderHttp.validateBaseUrl(baseUrl).startsWith("https://")
                                    ? AddressPolicy.PUBLIC_HTTPS
                                    : AddressPolicy.PRIVATE_NETWORK_HTTP),
                    1048576);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public ListWarehousesOperation(
                CredentialProvider<SecretString> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin = ProviderHttp.validateBaseUrl(baseUrl);
            if (!java.net.URI.create(origin).getRawPath().isEmpty())
                throw new IllegalArgumentException("Yijingtong tenant URL must be an origin");
            this.endpoint = origin + "/hwc_api/hwc_storage_list.php";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Yijingtong response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<WarehousePage> invoke(Call call, ListWarehousesInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal())) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Yijingtong token is unavailable or invalid");
            }
            try {
                ProviderHttpRequest request =
                        ProviderHttpRequest.newBuilder("POST", endpoint)
                                .body(
                                        JSON.writeValueAsBytes(
                                                java.util.Collections.singletonMap(
                                                        "token", key.reveal())))
                                .header("Accept", "application/json")
                                .header("Content-Type", "application/json; charset=utf-8")
                                .build();
                try (ProviderHttpResponse response = transport.send(request)) {
                    int status = response.getStatusCode();
                    if (status == 408 || status == 429 || status >= 500) {
                        return classifyStatus(response);
                    }
                    byte[] body;
                    try {
                        body = response.readBoundedBody(maxResponseBytes);
                    } catch (BodyTooLargeException error) {
                        return failed(
                                "invalidResponse",
                                FailureKind.RESPONSE_TOO_LARGE,
                                "Yijingtong response exceeded the configured limit");
                    } catch (IOException error) {
                        return retry(
                                FailureKind.AVAILABILITY,
                                "Yijingtong response was interrupted",
                                Duration.ZERO);
                    }
                    return decodeWarehouses(body, status);
                }
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "Yijingtong warehouse request failed",
                        Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/yijingtong/operation-definitions.json";
        try (InputStream input = WarehouseCatalog.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<WarehousePage> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(
                    FailureKind.RATE_LIMIT, "Yijingtong rate limited the warehouse request", delay);
        return retry(FailureKind.AVAILABILITY, "Yijingtong is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 2048
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static boolean whiteSpace(int c) {
        return c == 0x85 || Character.isSpaceChar(c) || (c >= 9 && c <= 13);
    }

    private static final class Response {
        public String error;
        public WireWarehouse[] data;
    }

    private static final class WireWarehouse {
        @JsonProperty("nums")
        public String id;

        @JsonProperty("pname")
        public String name;

        @JsonProperty("country")
        public String country;
    }

    private static QueryAttempt<WarehousePage> decodeWarehouses(byte[] body, int status) {
        if (status != 200) {
            FailureKind kind;
            switch (status) {
                case 401:
                    kind = FailureKind.AUTHENTICATION;
                    break;
                case 403:
                    kind = FailureKind.AUTHORIZATION;
                    break;
                case 400:
                case 404:
                case 412:
                    kind = FailureKind.VALIDATION;
                    break;
                default:
                    kind = FailureKind.PROVIDER_REJECTION;
            }
            return failed("providerRejected", kind, "Warehouse query rejected with HTTP " + status);
        }
        try {
            Response value = JSON.readValue(body, Response.class);
            if (value == null || !("T".equals(value.error) || "F".equals(value.error)))
                return invalid();
            if ("F".equals(value.error))
                return failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "Warehouse query rejected");
            WireWarehouse[] rows = value.data;
            if (rows == null || rows.length > 200) return invalid();
            java.util.List<WarehousePage.Warehouse> warehouses =
                    new java.util.ArrayList<>(rows.length);
            for (WireWarehouse row : rows) {
                if (row == null
                        || row.id == null
                        || row.name == null
                        || row.id.codePoints().allMatch(WarehouseCatalog::whiteSpace)
                        || row.name.codePoints().allMatch(WarehouseCatalog::whiteSpace))
                    return invalid();
                warehouses.add(
                        new WarehousePage.Warehouse(
                                row.id, row.name, row.country == null ? "" : row.country));
            }
            return QueryAttempt.branch("listed", new WarehousePage(warehouses), Receipt.empty());
        } catch (IOException error) {
            return invalid();
        }
    }

    private static QueryAttempt<WarehousePage> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid warehouse response");
    }

    private static QueryAttempt<WarehousePage> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new WarehousePage(java.util.Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<WarehousePage> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
