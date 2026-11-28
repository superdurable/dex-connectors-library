// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ctyun;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Objects;
import java.util.function.Function;

/** The CTYun knowledge-base query and its operation-specific Dex Step configuration. */
public final class KnowledgeBaseDirectory {
    private static final String CONNECTOR = "ctyun";
    private static final String OPERATION = "listKnowledgeBases";
    private static final String BASE_URL = "https://kqa-global.ctapi.ctyun.cn";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private KnowledgeBaseDirectory() {}

    /** The knowledge-base query takes an empty JSON object. */
    public static final class ListKnowledgeBasesInput {}

    /** Concrete Dex Step input for a knowledge-base list result. */
    public static final class ListKnowledgeBasesResult
            extends QueryResult<ListKnowledgeBasesOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListKnowledgeBasesResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListKnowledgeBasesOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the knowledge-base query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            ListKnowledgeBasesInput,
                            ListKnowledgeBasesOutput,
                            ListKnowledgeBasesResult>
                    newListKnowledgeBasesStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListKnowledgeBasesInput, ListKnowledgeBasesOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListKnowledgeBasesInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("CTYun connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListKnowledgeBasesResult::new);
    }

    /** CTYun access key, security key and tenant identifier resolved together for each attempt. */
    public static final class Credentials {
        private final SecretString accessKey, securityKey, tenantId;

        /** Bind the three provider-issued connection secrets. */
        public Credentials(
                SecretString accessKey, SecretString securityKey, SecretString tenantId) {
            this.accessKey = Objects.requireNonNull(accessKey);
            this.securityKey = Objects.requireNonNull(securityKey);
            this.tenantId = Objects.requireNonNull(tenantId);
        }

        boolean valid() {
            return accessKey.reveal().matches("[!-~]{1,2048}")
                    && securityKey.reveal().matches("[!-~]{1,2048}")
                    && tenantId.reveal().matches("[!-~]{1,2048}");
        }
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class ListKnowledgeBasesOperation
            implements Query<ListKnowledgeBasesInput, ListKnowledgeBasesOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final java.time.Clock clock;
        private final java.util.function.Supplier<String> requestId;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListKnowledgeBasesOperation(CredentialProvider<Credentials> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(
                            Duration.ofNanos(10000000000L), AddressPolicy.PUBLIC_HTTPS),
                    2097152);
        }

        /**
         * Bind a call-time credential source, configured transport, and response limit (positive
         * bytes).
         */
        public ListKnowledgeBasesOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(credentials, BASE_URL, transport, maxResponseBytes);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public ListKnowledgeBasesOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(
                    credentials,
                    baseUrl,
                    transport,
                    maxResponseBytes,
                    java.time.Clock.systemUTC(),
                    () -> java.util.UUID.randomUUID().toString());
        }

        ListKnowledgeBasesOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                java.time.Clock clock,
                java.util.function.Supplier<String> requestId) {
            this.clock = Objects.requireNonNull(clock);
            this.requestId = Objects.requireNonNull(requestId);
            this.endpoint =
                    ProviderHttp.validateBaseUrl(
                                    baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl)
                            + "/openapi/v1/infobases/list";
            if (!endpoint.equals(BASE_URL + "/openapi/v1/infobases/list"))
                throw new IllegalArgumentException("CTYun requires its documented API origin");
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("CTYun response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListKnowledgeBasesOutput> invoke(
                Call call, ListKnowledgeBasesInput input) throws InterruptedException {
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !key.valid()) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "CTYun credentials or tenant ID are unavailable or invalid");
            }
            String id;
            try {
                id = requestId.get();
            } catch (RuntimeException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "CTYun request ID could not be generated",
                        Duration.ZERO);
            }
            String date =
                    java.time.format.DateTimeFormatter.ofPattern(
                                    "yyyyMMdd'T'HHmmss'Z'", java.util.Locale.ROOT)
                            .withZone(java.time.ZoneOffset.ofHours(8))
                            .format(clock.instant());
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", endpoint)
                            .header("Ctyun-Eop-Request-Id", id)
                            .header("Eop-Date", date)
                            .header("Tenantid", key.tenantId.reveal())
                            .header("Eop-Authorization", sign(key, id, date))
                            .header("Accept", "application/json")
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
                            "CTYun response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "CTYun response was interrupted",
                            Duration.ZERO);
                }
                if (status != 200) return classifyStatus(response);
                return decodeKnowledgeBases(body, key);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "CTYun knowledge-base request failed",
                        Duration.ZERO);
            }
        }
    }

    private static byte[] mac(byte[] key, String message) {
        try {
            javax.crypto.Mac mac = javax.crypto.Mac.getInstance("HmacSHA256");
            mac.init(new javax.crypto.spec.SecretKeySpec(key, "HmacSHA256"));
            return mac.doFinal(message.getBytes(StandardCharsets.UTF_8));
        } catch (java.security.GeneralSecurityException error) {
            throw new IllegalStateException(error);
        }
    }

    private static String sign(Credentials keys, String id, String date) {
        String canonical =
                "ctyun-eop-request-id:"
                        + id
                        + "\neop-date:"
                        + date
                        + "\n\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";
        byte[] timeKey = mac(keys.securityKey.reveal().getBytes(StandardCharsets.UTF_8), date);
        byte[] dateKey = mac(mac(timeKey, keys.accessKey.reveal()), date.substring(0, 8));
        return keys.accessKey.reveal()
                + " headers=ctyun-eop-request-id;eop-date Signature="
                + java.util.Base64.getEncoder().encodeToString(mac(dateKey, canonical));
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/ctyun/operation-definitions.json";
        try (InputStream input = KnowledgeBaseDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListKnowledgeBasesOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(
                    FailureKind.RATE_LIMIT, "CTYun rate limited the knowledge-base request", delay);
        if (status == 408 || status >= 500)
            return retry(FailureKind.AVAILABILITY, "CTYun API is temporarily unavailable", delay);
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (status == 401) kind = FailureKind.AUTHENTICATION;
        if (status == 403) kind = FailureKind.AUTHENTICATION;
        return failed(
                "providerRejected",
                kind,
                "CTYun rejected knowledge-base request with HTTP " + status);
    }

    private static JsonNode field(JsonNode object, String name) {
        JsonNode match = com.fasterxml.jackson.databind.node.MissingNode.getInstance();
        java.util.Iterator<java.util.Map.Entry<String, JsonNode>> fields = object.fields();
        while (fields.hasNext()) {
            java.util.Map.Entry<String, JsonNode> entry = fields.next();
            if (name.equalsIgnoreCase(entry.getKey())) match = entry.getValue();
        }
        return match;
    }

    private static String string(JsonNode object, String field) {
        JsonNode value = field(object, field);
        if (value.isMissingNode() || value.isNull()) return "";
        if (!value.isTextual()) throw new IllegalArgumentException();
        return value.textValue();
    }

    private static long integer(JsonNode object, String field) {
        JsonNode value = field(object, field);
        if (value.isMissingNode() || value.isNull()) return 0;
        if (!value.isIntegralNumber() || !value.canConvertToLong())
            throw new IllegalArgumentException();
        return value.longValue();
    }

    private static int size(String text) {
        return text.getBytes(StandardCharsets.UTF_8).length;
    }

    private static QueryAttempt<ListKnowledgeBasesOutput> decodeKnowledgeBases(
            byte[] body, Credentials credential) {
        try {
            JsonNode envelope = JSON.readTree(body);
            if (envelope == null || !envelope.isObject()) throw new IllegalArgumentException();
            JsonNode code = field(envelope, "statusCode");
            String status =
                    code.isTextual()
                            ? code.textValue()
                            : (code.isIntegralNumber()
                                            && code.canConvertToLong()
                                            && code.longValue() > 0
                                    ? code.asText()
                                    : "");
            if (status.isEmpty()) throw new IllegalArgumentException();
            String error = string(envelope, "error");
            JsonNode rows = field(envelope, "returnObj");
            if (!rows.isMissingNode() && !rows.isNull() && !rows.isArray())
                throw new IllegalArgumentException();
            List<ListKnowledgeBasesOutput.KnowledgeBaseSummary> output = new ArrayList<>();
            for (JsonNode row : rows) {
                if (!row.isObject() && !row.isNull()) throw new IllegalArgumentException();
                output.add(
                        new ListKnowledgeBasesOutput.KnowledgeBaseSummary(
                                integer(row, "id"), string(row, "name"), integer(row, "count")));
            }
            if (!status.equals("200")) {
                if (status.equals("40002") || status.equals("40004"))
                    return retry(
                            FailureKind.AVAILABILITY,
                            "CTYun service reported a temporary error",
                            Duration.ZERO);
                boolean auth =
                        status.equals("40001")
                                || status.equals("40003")
                                || status.equals("40016")
                                || error.startsWith("CTAPI_100");
                return failed(
                        "providerRejected",
                        auth ? FailureKind.AUTHENTICATION : FailureKind.PROVIDER_REJECTION,
                        "CTYun rejected the knowledge-base request");
            }
            if (!rows.isArray()) throw new IllegalArgumentException();
            for (ListKnowledgeBasesOutput.KnowledgeBaseSummary item : output) {
                if (item.id <= 0
                        || item.name.isEmpty()
                        || size(item.name) > 1024
                        || item.fileCount < 0
                        || item.name.contains(credential.securityKey.reveal())
                        || item.name.contains(credential.accessKey.reveal())
                        || item.name.contains(credential.tenantId.reveal()))
                    throw new IllegalArgumentException();
            }
            return QueryAttempt.branch(
                    "listed", new ListKnowledgeBasesOutput(output), Receipt.empty());
        } catch (IOException | IllegalArgumentException error) {
            return failed(
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "CTYun returned invalid knowledge-base metadata");
        }
    }

    private static QueryAttempt<ListKnowledgeBasesOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListKnowledgeBasesOutput(Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListKnowledgeBasesOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
