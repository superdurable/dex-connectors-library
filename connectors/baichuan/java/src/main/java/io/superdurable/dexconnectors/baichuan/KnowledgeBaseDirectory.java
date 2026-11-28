// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.baichuan;

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

/** The Baichuan knowledge-base query and its operation-specific Dex Step configuration. */
public final class KnowledgeBaseDirectory {
    private static final String CONNECTOR = "baichuan";
    private static final String OPERATION = "listKnowledgeBases";
    private static final String BASE_URL = "https://api.baichuan-ai.com";
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
            throw new IllegalArgumentException("Baichuan connection provider is invalid");
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

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class ListKnowledgeBasesOperation
            implements Query<ListKnowledgeBasesInput, ListKnowledgeBasesOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListKnowledgeBasesOperation(CredentialProvider<SecretString> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(
                            Duration.ofNanos(10000000000L), AddressPolicy.PUBLIC_HTTPS),
                    1048576);
        }

        /**
         * Bind a call-time credential source, configured transport, and response limit (positive
         * bytes).
         */
        public ListKnowledgeBasesOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(credentials, BASE_URL, transport, maxResponseBytes);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public ListKnowledgeBasesOperation(
                CredentialProvider<SecretString> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.endpoint =
                    ProviderHttp.validateBaseUrl(
                                    baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl)
                            + "/v1/kbs";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Baichuan response limit must be positive");
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
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !key.reveal().matches("[!-~]{1,512}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Baichuan API Key is unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", endpoint)
                            .header("Authorization", "Bearer " + key.reveal())
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status != 200) {
                    return classifyStatus(response);
                }
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Baichuan response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Baichuan response was interrupted",
                            Duration.ZERO);
                }
                return decodeKnowledgeBases(body, key);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "Baichuan knowledge-base request failed",
                        Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/baichuan/operation-definitions.json";
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
                    FailureKind.RATE_LIMIT,
                    "Baichuan rate limited the knowledge-base request",
                    delay);
        if (status == 408 || status >= 500)
            return retry(
                    FailureKind.AVAILABILITY, "Baichuan API is temporarily unavailable", delay);
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (status == 401) kind = FailureKind.AUTHENTICATION;
        if (status == 403) kind = FailureKind.AUTHORIZATION;
        return failed(
                "providerRejected",
                kind,
                "Baichuan rejected knowledge-base request with HTTP " + status);
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
            byte[] body, SecretString credential) {
        try {
            JsonNode envelope = JSON.readTree(body);
            if (envelope == null || !envelope.isObject()) throw new IllegalArgumentException();
            if (!"list".equals(string(envelope, "object"))) throw new IllegalArgumentException();
            JsonNode rows = field(envelope, "data");
            if (!rows.isArray() || rows.size() > 10000) throw new IllegalArgumentException();
            java.util.Set<String> seen = new java.util.HashSet<>();
            List<ListKnowledgeBasesOutput.KnowledgeBase> output = new ArrayList<>();
            for (JsonNode row : rows) {
                String id = string(row, "id"),
                        name = string(row, "name"),
                        description = string(row, "description"),
                        status = string(row, "status");
                long created = integer(row, "created_at"), updated = integer(row, "updated_at");
                if (id.isEmpty()
                        || size(id) > 256
                        || !seen.add(id)
                        || !"knowledge-base".equals(string(row, "object"))
                        || size(name) > 256
                        || size(description) > 4096
                        || created < 0
                        || updated < 0) throw new IllegalArgumentException();
                List<String> files = new ArrayList<>();
                JsonNode ids = field(row, "file_ids");
                if (!ids.isMissingNode() && !ids.isNull()) {
                    if (!ids.isArray() || ids.size() > 10000) throw new IllegalArgumentException();
                    for (JsonNode node : ids) {
                        String file = node.textValue();
                        if (file == null
                                || file.isEmpty()
                                || size(file) > 256
                                || file.contains(credential.reveal()))
                            throw new IllegalArgumentException();
                        files.add(file);
                    }
                }
                for (String field : new String[] {id, name, description, status})
                    if (field.contains(credential.reveal())) throw new IllegalArgumentException();
                output.add(
                        new ListKnowledgeBasesOutput.KnowledgeBase(
                                id, name, description, status, files, created, updated));
            }
            return QueryAttempt.branch(
                    "listed", new ListKnowledgeBasesOutput(output), Receipt.empty());
        } catch (IOException | IllegalArgumentException error) {
            return failed(
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "Baichuan returned invalid knowledge-base metadata");
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
