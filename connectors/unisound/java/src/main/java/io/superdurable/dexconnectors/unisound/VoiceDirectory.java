// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.unisound;

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

/** The Unisound voice query and its operation-specific Dex Step configuration. */
public final class VoiceDirectory {
    private static final String CONNECTOR = "unisound";
    private static final String OPERATION = "listSystemVoices";
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

    private VoiceDirectory() {}

    /** Empty input for the listSystemVoices query. */
    public static final class ListSystemVoicesInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Concrete Dex Step input for a voice result. */
    public static final class ListSystemVoicesResult extends QueryResult<ListSystemVoicesOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListSystemVoicesResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListSystemVoicesOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the voice query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            ListSystemVoicesInput,
                            ListSystemVoicesOutput,
                            ListSystemVoicesResult>
                    newListSystemVoicesStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListSystemVoicesInput, ListSystemVoicesOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListSystemVoicesInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Unisound connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListSystemVoicesResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListSystemVoicesOperation fromProject(
            LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "unisound".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Unisound connection is missing"));
        if (!"unisound".equals(selected.getProvider()))
            throw new IllegalArgumentException("Unisound connection provider differs");
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
                        throw new IllegalArgumentException("Invalid Unisound timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Unisound response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Unisound configuration field");
            }
        }
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("unisound", connectionName),
                        material -> {
                            JsonNode token = material.get("apiKey");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Unisound credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListSystemVoicesOperation(
                provider,
                new ProviderHttpClient(Duration.ofNanos(timeout), AddressPolicy.PUBLIC_HTTPS),
                limit);
    }

    /** Reads one voice list using bounded responses and a redirect-safe transport. */
    public static final class ListSystemVoicesOperation
            implements Query<ListSystemVoicesInput, ListSystemVoicesOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private static final String ENDPOINT =
                "https://maas-api.unisound.com/v1/audio/voices/query";
        private final int maxResponseBytes;

        /** Use the fixed provider endpoint with the manifest timeout and response limit. */
        public ListSystemVoicesOperation(CredentialProvider<SecretString> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                    1 << 20);
        }

        /**
         * Bind the credential provider, caller-owned transport and maximum response size in bytes.
         */
        public ListSystemVoicesOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > (16 << 20)) {
                throw new IllegalArgumentException("Unisound response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListSystemVoicesOutput> invoke(Call call, ListSystemVoicesInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid Unisound API token");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("POST", ENDPOINT)
                            .header("Accept", "application/json")
                            .header("Content-Type", "application/json")
                            .body(
                                    "{\"voice_type\":\"system\"}"
                                            .getBytes(java.nio.charset.StandardCharsets.UTF_8))
                            .header("Authorization", "Bearer " + key.reveal())
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
                            "Unisound rejected the request");
                byte[] body = response.readBoundedBody(maxResponseBytes);
                try {
                    return decodeVoices(body);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Unisound response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Unisound request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/unisound/operation-definitions.json";
        try (InputStream input = VoiceDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListSystemVoicesOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(
                    FailureKind.RATE_LIMIT, "Unisound rate limited the request", delay);
        return retry(FailureKind.AVAILABILITY, "Unisound is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 1024
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
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

    private static final class DirectoryResponse {
        public DirectoryData base_resp;
        public WireItem[] system_voice;
    }

    private static final class DirectoryData {
        public Long status_code;
    }

    private static final class WireItem {
        public String voice_id;
        public String voice_name;
        public String[] language;
    }

    private static QueryAttempt<ListSystemVoicesOutput> decodeVoices(byte[] body)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null || page.base_resp == null) return invalid();
        Long code = page.base_resp.status_code;
        if (code == null) return invalid();
        if (code == 100501)
            return retry(FailureKind.RATE_LIMIT, "Unisound rate limited the query", Duration.ZERO);
        if (code == 100999)
            return retry(
                    FailureKind.AVAILABILITY, "Unisound is temporarily unavailable", Duration.ZERO);
        if (code != 0)
            return failed(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "Unisound rejected the query");
        WireItem[] items = page.system_voice;
        if (items == null || items.length > 1000) return invalid();
        java.util.List<ListSystemVoicesOutput.SystemVoice> values = new java.util.ArrayList<>();
        for (WireItem item : items) {
            if (values.size() == 25) break;
            if (item == null) return invalid();
            if (item.voice_id == null
                    || item.voice_id.isEmpty()
                    || hasEdgeSpace(item.voice_id)
                    || length(item.voice_id) > 128) return invalid();
            if (item.voice_name == null) item.voice_name = "";
            if (length(item.voice_name) > 200) return invalid();
            if (item.language == null || item.language.length > 20) return invalid();
            java.util.List<String> languages = new java.util.ArrayList<>();
            for (String lang : item.language) languages.add(lang == null ? "" : lang);
            values.add(
                    new ListSystemVoicesOutput.SystemVoice(
                            item.voice_id, item.voice_name, languages));
        }
        return QueryAttempt.branch("found", new ListSystemVoicesOutput(values), Receipt.empty());
    }

    private static QueryAttempt<ListSystemVoicesOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid voice response");
    }

    private static QueryAttempt<ListSystemVoicesOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListSystemVoicesOutput(java.util.Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListSystemVoicesOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
