// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.winit;

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

/** The Winit warehouse query and its operation-specific Dex Step configuration. */
public final class WarehouseCatalog {
    private static final String CONNECTOR = "winit";
    private static final String OPERATION = "listWarehouses";
    private static final String BASE_URL = "https://openapi.winit.com.cn";
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
            throw new IllegalArgumentException("Winit connection provider is invalid");
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

    /** Provider credentials resolved for each warehouse query. */
    public static final class Credentials {
        /** Provider app key. */
        public final SecretString appKey;

        /** Provider token. */
        public final SecretString token;

        /** Provider client id. */
        public final SecretString clientId;

        /** Provider client secret. */
        public final SecretString clientSecret;

        /** Bind the provider account and signing credentials. */
        public Credentials(
                SecretString appKey,
                SecretString token,
                SecretString clientId,
                SecretString clientSecret) {
            this.appKey = appKey;
            this.token = token;
            this.clientId = clientId;
            this.clientSecret = clientSecret;
        }
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class ListWarehousesOperation
            implements Query<ListWarehousesInput, WarehousePage> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        private final java.time.Clock clock;
        private final String platform;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListWarehousesOperation(
                CredentialProvider<Credentials> credentials, String platform) {
            this(
                    credentials,
                    BASE_URL,
                    platform,
                    new ProviderHttpClient(Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                    1048576,
                    java.time.Clock.systemUTC());
        }

        /**
         * Bind the configured origin, transport, positive byte limit, and request signing clock.
         */
        public ListWarehousesOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                String platform,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                java.time.Clock clock) {
            this.clock = Objects.requireNonNull(clock);
            if (platform == null || !platform.matches("[A-Za-z0-9_-]{1,128}"))
                throw new IllegalArgumentException("Invalid Winit platform");
            this.platform = platform;
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            this.endpoint = origin + "/openapi/service";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Winit response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        private ProviderHttpRequest signedRequest(Credentials key) throws IOException {
            String timestamp =
                    java.time.format.DateTimeFormatter.ofPattern(
                                    "yyyy-MM-dd HH:mm:ss", java.util.Locale.ROOT)
                            .withZone(java.time.ZoneOffset.ofHours(8))
                            .format(clock.instant());
            java.util.SortedMap<String, String> fields = new java.util.TreeMap<>();
            fields.put("action", "queryWarehouse");
            fields.put("app_key", key.appKey.reveal());
            fields.put("format", "json");
            fields.put("platform", platform);
            fields.put("sign_method", "md5");
            fields.put("timestamp", timestamp);
            fields.put("version", "1.0");
            StringBuilder message = new StringBuilder();
            fields.forEach((name, value) -> message.append(name).append(value));
            String signed = message.toString();
            fields.put(
                    "client_sign",
                    digest(key.clientSecret.reveal() + signed + key.clientSecret.reveal()));
            fields.put("sign", digest(key.token.reveal() + signed + key.token.reveal()));
            fields.put("client_id", key.clientId.reveal());
            return ProviderHttpRequest.newBuilder("POST", endpoint)
                    .header("Accept", "application/json")
                    .header("Content-Type", "application/json")
                    .body(JSON.writeValueAsBytes(fields))
                    .build();
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<WarehousePage> invoke(Call call, ListWarehousesInput input)
                throws InterruptedException {
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null
                    || key.appKey == null
                    || !validKey(key.appKey.reveal())
                    || key.token == null
                    || !validKey(key.token.reveal())
                    || key.clientId == null
                    || !validKey(key.clientId.reveal())
                    || key.clientSecret == null
                    || !validKey(key.clientSecret.reveal())) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Winit token is unavailable or invalid");
            }
            ProviderHttpRequest request;
            try {
                request = signedRequest(key);
            } catch (IOException error) {
                return failed("defect", FailureKind.VALIDATION, "Warehouse request is invalid");
            }
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
                            "Winit response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Winit response was interrupted",
                            Duration.ZERO);
                }
                return decodeWarehouses(body, status);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "Winit warehouse request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/winit/operation-definitions.json";
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
            return retry(FailureKind.RATE_LIMIT, "Winit rate limited the warehouse request", delay);
        return retry(FailureKind.AVAILABILITY, "Winit is temporarily unavailable", delay);
    }

    private static String digest(String message) {
        try {
            byte[] raw =
                    java.security.MessageDigest.getInstance("MD5")
                            .digest(message.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            StringBuilder hex = new StringBuilder(32);
            for (byte octet : raw) hex.append(String.format(java.util.Locale.ROOT, "%02X", octet));
            return hex.toString();
        } catch (java.security.NoSuchAlgorithmException error) {
            throw new IllegalStateException(error);
        }
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 2048
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class Response {
        public Long code;
        public WireWarehouse[] data;
    }

    private static final class WireWarehouse {
        @JsonProperty("warehouseID")
        public Long id;

        @JsonProperty("warehouseCode")
        public String code;

        @JsonProperty("warehouseName")
        public String name;
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
                    kind = FailureKind.VALIDATION;
                    break;
                default:
                    kind = FailureKind.PROVIDER_REJECTION;
            }
            return failed("providerRejected", kind, "Warehouse query rejected with HTTP " + status);
        }
        try {
            Response value = JSON.readValue(body, Response.class);
            if (value == null || value.code == null) return invalid();
            if (value.code != 0)
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
                        || row.id <= 0
                        || row.code == null
                        || row.code.isEmpty()
                        || row.name == null
                        || row.name.isEmpty()) return invalid();
                warehouses.add(new WarehousePage.Warehouse(row.id, row.code, row.name));
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
