// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.likelic;

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

/** The Likelic room query and its operation-specific Dex Step configuration. */
public final class RoomDirectory {
    private static final String CONNECTOR = "likelic";
    private static final String OPERATION = "listRooms";
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

    private RoomDirectory() {}

    /** Select a page of rooms visible to the customer. */
    public static final class ListRoomsInput {
        /** One-based page, from 1 through 1000000. */
        public final long page;

        /** Select a provider page. */
        @JsonCreator
        public ListRoomsInput(@JsonProperty("page") long page) {
            this.page = page;
        }
    }

    /** Concrete Dex Step input for a room result. */
    public static final class ListRoomsResult extends QueryResult<ListRoomsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListRoomsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListRoomsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the room query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListRoomsInput, ListRoomsOutput, ListRoomsResult>
                    newListRoomsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListRoomsInput, ListRoomsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListRoomsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Likelic connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListRoomsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListRoomsOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "likelic".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Likelic connection is missing"));
        if (!"likelic".equals(selected.getProvider()))
            throw new IllegalArgumentException("Likelic connection provider differs");
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
                        throw new IllegalArgumentException("Invalid Likelic timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Likelic response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Likelic configuration field");
            }
        }
        ProjectCredentialProvider<Credentials> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("likelic", connectionName),
                        material -> {
                            JsonNode customerCode = material.get("customerCode"),
                                    apiKey = material.get("apiKey");
                            if (material.size() != 2
                                    || customerCode == null
                                    || !customerCode.isTextual()
                                    || apiKey == null
                                    || !apiKey.isTextual())
                                throw new IllegalArgumentException(
                                        "Invalid Likelic customer credentials");
                            return new Credentials(
                                    customerCode.textValue(), new SecretString(apiKey.textValue()));
                        },
                        null);
        return new ListRoomsOperation(
                provider,
                new ProviderHttpClient(Duration.ofNanos(timeout), AddressPolicy.PUBLIC_HTTPS),
                limit);
    }

    /** Customer code and API key resolved together for each room query. */
    public static final class Credentials {
        /** Nonsecret Likelic customer code. */
        public final String customerCode;

        /** API key paired with the customer code. */
        public final SecretString apiKey;

        /** Pair the provider's customer code with its API key. */
        public Credentials(String customerCode, SecretString apiKey) {
            this.customerCode = Objects.requireNonNull(customerCode);
            this.apiKey = Objects.requireNonNull(apiKey);
        }
    }

    /** Reads one room list using bounded responses and a redirect-safe transport. */
    public static final class ListRoomsOperation implements Query<ListRoomsInput, ListRoomsOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final int maxResponseBytes;

        /** Use the fixed provider endpoint with the manifest timeout and response limit. */
        public ListRoomsOperation(CredentialProvider<Credentials> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                    1 << 20);
        }

        /** Bind the credential source, transport and maximum response size in bytes. */
        public ListRoomsOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > (16 << 20)) {
                throw new IllegalArgumentException("Likelic response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListRoomsOutput> invoke(Call call, ListRoomsInput input)
                throws InterruptedException {
            if (input.page < 1 || input.page > 1000000)
                return failed(
                        "defect",
                        FailureKind.VALIDATION,
                        "Room page must be from 1 through 1000000");
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null
                    || !key.customerCode.matches("[A-Za-z0-9_-]{1,50}")
                    || !key.apiKey.reveal().matches("sk-[A-Fa-f0-9]{48}"))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Invalid Likelic customer credentials");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET", ENDPOINT + "?page=" + input.page + "&limit=20")
                            .header("Accept", "application/json")
                            .header("X-Customer-Code", key.customerCode)
                            .header("X-Api-Key", key.apiKey.reveal())
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
                            "Likelic rejected the request");
                byte[] body = response.readBoundedBody(maxResponseBytes);
                try {
                    return decodeRooms(body, input.page);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Likelic response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Likelic request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/likelic/operation-definitions.json";
        try (InputStream input = RoomDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListRoomsOutput> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Likelic rate limited the request", delay);
        return retry(FailureKind.AVAILABILITY, "Likelic is temporarily unavailable", delay);
    }

    private static final String ENDPOINT = "https://agent.likelic.com/api/v1/rooms";

    private static final class RoomResponse {
        public Boolean success;
        public RoomItem[] data;
        public PageMetadata meta;
    }

    private static final class PageMetadata {
        public Long page, limit, total;
    }

    private static final class RoomItem {
        public String channel, room_id, push_state;
        public Long instance_id;
    }

    private static QueryAttempt<ListRoomsOutput> decodeRooms(byte[] body, long page)
            throws IOException {
        RoomResponse response = JSON.readValue(body, RoomResponse.class);
        if (response == null
                || !Boolean.TRUE.equals(response.success)
                || response.data == null
                || response.data.length > 20
                || response.meta == null) return invalid();
        PageMetadata meta = response.meta;
        long total = meta.total == null ? 0 : meta.total;
        if (meta.page == null
                || meta.page != page
                || meta.limit == null
                || meta.limit != 20
                || total < 0) return invalid();
        java.util.List<ListRoomsOutput.Room> rooms = new java.util.ArrayList<>();
        for (RoomItem room : response.data) {
            if (room == null
                    || !("wework".equals(room.channel) || "qq".equals(room.channel))
                    || room.instance_id == null
                    || room.instance_id <= 0
                    || room.room_id == null
                    || room.room_id.isEmpty()
                    || room.room_id.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 256
                    || room.room_id.indexOf(0) >= 0
                    || room.room_id.indexOf('\r') >= 0
                    || room.room_id.indexOf('\n') >= 0
                    || !("ready".equals(room.push_state)
                            || "blocked".equals(room.push_state)
                            || "unknown".equals(room.push_state))) return invalid();
            rooms.add(
                    new ListRoomsOutput.Room(
                            room.channel, room.instance_id, room.room_id, room.push_state));
        }
        return QueryAttempt.branch(
                "found", new ListRoomsOutput(rooms, total, page), Receipt.empty());
    }

    private static QueryAttempt<ListRoomsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid room response");
    }

    private static QueryAttempt<ListRoomsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListRoomsOutput(java.util.Collections.emptyList(), 0, 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListRoomsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
