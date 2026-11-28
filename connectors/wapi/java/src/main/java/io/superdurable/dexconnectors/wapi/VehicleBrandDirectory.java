// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.wapi;

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

/** The Wapi vehicle brand query and its operation-specific Dex Step configuration. */
public final class VehicleBrandDirectory {
    private static final String CONNECTOR = "wapi";
    private static final String OPERATION = "listVehicleBrands";
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

    private VehicleBrandDirectory() {}

    /** Empty input for the listVehicleBrands query. */
    public static final class ListVehicleBrandsInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Concrete Dex Step input for a vehicle brand result. */
    public static final class ListVehicleBrandsResult extends QueryResult<ListVehicleBrandsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListVehicleBrandsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListVehicleBrandsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the vehicle brand query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            ListVehicleBrandsInput,
                            ListVehicleBrandsOutput,
                            ListVehicleBrandsResult>
                    newListVehicleBrandsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListVehicleBrandsInput, ListVehicleBrandsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListVehicleBrandsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Wapi connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListVehicleBrandsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListVehicleBrandsOperation fromProject(
            LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "wapi".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("Wapi connection is missing"));
        if (!"wapi".equals(selected.getProvider()))
            throw new IllegalArgumentException("Wapi connection provider differs");
        long timeout = 15000000000L;
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
                        throw new IllegalArgumentException("Invalid Wapi timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Wapi response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Wapi configuration field");
            }
        }
        ProjectCredentialProvider<Credentials> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("wapi", connectionName),
                        material -> {
                            JsonNode appId = material.get("appID"),
                                    appSecret = material.get("appSecret");
                            if (material.size() != 2
                                    || appId == null
                                    || !appId.isTextual()
                                    || appSecret == null
                                    || !appSecret.isTextual())
                                throw new IllegalArgumentException(
                                        "Invalid Wapi application credentials");
                            return new Credentials(
                                    appId.textValue(), new SecretString(appSecret.textValue()));
                        },
                        null);
        return new ListVehicleBrandsOperation(
                provider,
                new ProviderHttpClient(Duration.ofNanos(timeout), AddressPolicy.PUBLIC_HTTPS),
                limit,
                java.time.Clock.systemUTC());
    }

    /** Application identity and secret resolved together for each signed request. */
    public static final class Credentials {
        /** Nonsecret Wapi application identifier. */
        public final String appID;

        /** Secret used only to sign the provider request. */
        public final SecretString appSecret;

        /** Pair the provider's application ID with its secret. */
        public Credentials(String appID, SecretString appSecret) {
            this.appID = Objects.requireNonNull(appID);
            this.appSecret = Objects.requireNonNull(appSecret);
        }
    }

    /** Reads one vehicle brand list using bounded responses and a redirect-safe transport. */
    public static final class ListVehicleBrandsOperation
            implements Query<ListVehicleBrandsInput, ListVehicleBrandsOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final java.time.Clock clock;
        private final int maxResponseBytes;

        /** Use the fixed provider endpoint with the manifest timeout and response limit. */
        public ListVehicleBrandsOperation(CredentialProvider<Credentials> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                    1 << 20,
                    java.time.Clock.systemUTC());
        }

        /** Bind the credentials, transport, byte limit and clock used to sign each request. */
        public ListVehicleBrandsOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                java.time.Clock clock) {
            this.clock = Objects.requireNonNull(clock);
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > (16 << 20)) {
                throw new IllegalArgumentException("Wapi response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListVehicleBrandsOutput> invoke(Call call, ListVehicleBrandsInput input)
                throws InterruptedException {
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null
                    || !key.appID.matches("[0-9]{1,20}")
                    || !key.appSecret.reveal().matches("[A-Za-z0-9]{32}"))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Invalid Wapi application credentials");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET", signedEndpoint(key, clock.instant().getEpochSecond()))
                            .header("Accept", "application/json")
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
                            "Wapi rejected the request");
                byte[] body = response.readBoundedBody(maxResponseBytes);
                try {
                    return decodeBrands(body);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Wapi response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Wapi request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/wapi/operation-definitions.json";
        try (InputStream input = VehicleBrandDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListVehicleBrandsOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Wapi rate limited the request", delay);
        return retry(FailureKind.AVAILABILITY, "Wapi is temporarily unavailable", delay);
    }

    private static final String ENDPOINT = "https://uaqy.api.storeapi.net/pyi/170/324";

    private static String signedEndpoint(Credentials credentials, long seconds) {
        String material =
                "appid"
                        + credentials.appID
                        + "formatjsontime"
                        + seconds
                        + credentials.appSecret.reveal();
        byte[] digest;
        try {
            digest =
                    java.security.MessageDigest.getInstance("MD5")
                            .digest(material.getBytes(java.nio.charset.StandardCharsets.US_ASCII));
        } catch (java.security.NoSuchAlgorithmException unavailable) {
            throw new IllegalStateException("MD5 is required by Wapi", unavailable);
        }
        StringBuilder signature = new StringBuilder(32);
        for (byte value : digest) {
            signature
                    .append(Character.forDigit((value >>> 4) & 15, 16))
                    .append(Character.forDigit(value & 15, 16));
        }
        return ENDPOINT
                + "?appid="
                + credentials.appID
                + "&format=json&sign="
                + signature
                + "&time="
                + seconds;
    }

    private static final class DirectoryResponse {
        public String codeid;
        public WireItem[] retdata;
    }

    private static final class WireItem {
        public String ppid;
        public String pp_name;
        public Long pp_price;
    }

    private static QueryAttempt<ListVehicleBrandsOutput> decodeBrands(byte[] body)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null) return invalid();
        if (page.codeid == null || page.codeid.isEmpty()) return invalid();
        if ("10021".equals(page.codeid) || "10014".equals(page.codeid))
            return retry(
                    FailureKind.AVAILABILITY, "Wapi is temporarily unavailable", Duration.ZERO);
        if (!"10000".equals(page.codeid))
            return failed(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, "Wapi rejected the query");
        WireItem[] items = page.retdata;
        if (items == null || items.length > 5000) return invalid();
        java.util.List<ListVehicleBrandsOutput.VehicleBrandPrice> values =
                new java.util.ArrayList<>();
        for (WireItem item : items) {
            if (item == null) return invalid();
            if (item.pp_price == null) item.pp_price = 0L;
            if (item.ppid == null
                    || item.ppid.isEmpty()
                    || item.ppid.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 50
                    || item.pp_name == null
                    || item.pp_name.isEmpty()
                    || item.pp_name.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 256
                    || item.pp_price < 0) return invalid();
            if (values.size() == 200) continue;
            values.add(
                    new ListVehicleBrandsOutput.VehicleBrandPrice(
                            item.ppid, item.pp_name, item.pp_price));
        }
        return QueryAttempt.branch(
                "found", new ListVehicleBrandsOutput(values, items.length), Receipt.empty());
    }

    private static QueryAttempt<ListVehicleBrandsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid vehicle brand response");
    }

    private static QueryAttempt<ListVehicleBrandsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListVehicleBrandsOutput(java.util.Collections.emptyList(), 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListVehicleBrandsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
