// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zilliz;

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

/** The Zilliz cluster query and its operation-specific Dex Step configuration. */
public final class ClusterDirectory {
    private static final String CONNECTOR = "zilliz";
    private static final String OPERATION = "listClusters";
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

    private ClusterDirectory() {}

    /** Empty input for the listClusters query. */
    public static final class ListClustersInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Concrete Dex Step input for a cluster result. */
    public static final class ListClustersResult extends QueryResult<ListClustersOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListClustersResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListClustersOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the cluster query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListClustersInput, ListClustersOutput, ListClustersResult>
                    newListClustersStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListClustersInput, ListClustersOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListClustersInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Zilliz connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListClustersResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListClustersOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "zilliz".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("Zilliz connection is missing"));
        if (!"zilliz".equals(selected.getProvider()))
            throw new IllegalArgumentException("Zilliz connection provider differs");
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
                        throw new IllegalArgumentException("Invalid Zilliz timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Zilliz response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Zilliz configuration field");
            }
        }
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("zilliz", connectionName),
                        material -> {
                            JsonNode token = material.get("apiKey");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Zilliz credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListClustersOperation(
                provider,
                new ProviderHttpClient(Duration.ofNanos(timeout), AddressPolicy.PUBLIC_HTTPS),
                limit);
    }

    /** Reads one cluster list using bounded responses and a redirect-safe transport. */
    public static final class ListClustersOperation
            implements Query<ListClustersInput, ListClustersOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private static final String ENDPOINT =
                "https://api.cloud.zilliz.com/v2/clusters?pageSize=25&currentPage=1";
        private final int maxResponseBytes;

        /** Use the fixed provider endpoint with the manifest timeout and response limit. */
        public ListClustersOperation(CredentialProvider<SecretString> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                    1 << 20);
        }

        /**
         * Bind the credential provider, caller-owned transport and maximum response size in bytes.
         */
        public ListClustersOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > (16 << 20)) {
                throw new IllegalArgumentException("Zilliz response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListClustersOutput> invoke(Call call, ListClustersInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid Zilliz API token");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", ENDPOINT)
                            .header("Accept", "application/json")
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
                            "Zilliz rejected the request");
                byte[] body = response.readBoundedBody(maxResponseBytes);
                try {
                    return decodeClusters(body);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Zilliz response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Zilliz request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/zilliz/operation-definitions.json";
        try (InputStream input = ClusterDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListClustersOutput> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Zilliz rate limited the request", delay);
        return retry(FailureKind.AVAILABILITY, "Zilliz is temporarily unavailable", delay);
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
        public Long code;
        public DirectoryData data;
    }

    private static final class DirectoryData {
        public Long count;
        public WireItem[] clusters;
    }

    private static final class WireItem {
        public String clusterId;
        public String clusterName;
        public String regionId;
        public String plan;
        public String status;
    }

    private static QueryAttempt<ListClustersOutput> decodeClusters(byte[] body) throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null) return invalid();
        Long code = page.code;
        if (code == null) return invalid();
        if (code != 0)
            return failed(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "Zilliz rejected the query");
        if (page.data == null || page.data.count == null || page.data.count < 0) return invalid();
        WireItem[] items = page.data.clusters;
        if (items == null || items.length > 25) return invalid();
        java.util.List<ListClustersOutput.ClusterSummary> values = new java.util.ArrayList<>();
        for (WireItem item : items) {
            if (item == null) return invalid();
            if (item.clusterId == null
                    || item.clusterId.isEmpty()
                    || hasEdgeSpace(item.clusterId)
                    || length(item.clusterId) > 128) return invalid();
            if (item.clusterName == null) item.clusterName = "";
            if (length(item.clusterName) > 200) return invalid();
            if (item.regionId == null) item.regionId = "";
            if (length(item.regionId) > 100) return invalid();
            if (item.plan == null) item.plan = "";
            if (length(item.plan) > 100) return invalid();
            if (item.status == null) item.status = "";
            if (length(item.status) > 100) return invalid();
            values.add(
                    new ListClustersOutput.ClusterSummary(
                            item.clusterId,
                            item.clusterName,
                            item.regionId,
                            item.plan,
                            item.status));
        }
        return QueryAttempt.branch(
                "found", new ListClustersOutput(values, page.data.count), Receipt.empty());
    }

    private static QueryAttempt<ListClustersOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid cluster response");
    }

    private static QueryAttempt<ListClustersOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListClustersOutput(java.util.Collections.emptyList(), 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListClustersOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
