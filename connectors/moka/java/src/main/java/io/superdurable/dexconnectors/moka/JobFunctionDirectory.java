// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.moka;

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

/** The Moka job function query and its operation-specific Dex Step configuration. */
public final class JobFunctionDirectory {
    private static final String CONNECTOR = "moka";
    private static final String OPERATION = "listJobFunctions";
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

    private JobFunctionDirectory() {}

    /** Empty input for the listJobFunctions query. */
    public static final class ListJobFunctionsInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Concrete Dex Step input for a job function result. */
    public static final class ListJobFunctionsResult extends QueryResult<ListJobFunctionsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListJobFunctionsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListJobFunctionsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the job function query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            ListJobFunctionsInput,
                            ListJobFunctionsOutput,
                            ListJobFunctionsResult>
                    newListJobFunctionsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListJobFunctionsInput, ListJobFunctionsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListJobFunctionsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Moka connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListJobFunctionsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListJobFunctionsOperation fromProject(
            LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "moka".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("Moka connection is missing"));
        if (!"moka".equals(selected.getProvider()))
            throw new IllegalArgumentException("Moka connection provider differs");
        String organizationId = "", mode = "social";
        long timeout = 10000000000L;
        int limit = 1 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "orgId":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Moka organization identifier");
                    organizationId = value.textValue();
                    break;
                case "mode":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Moka recruiting mode");
                    mode = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Moka timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Moka response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Moka configuration field");
            }
        }
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("moka", connectionName),
                        material -> {
                            JsonNode token = material.get("api_key");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Moka credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListJobFunctionsOperation(
                provider,
                organizationId,
                mode,
                new ProviderHttpClient(Duration.ofNanos(timeout), AddressPolicy.PUBLIC_HTTPS),
                limit);
    }

    /** Reads one job function list using bounded responses and a redirect-safe transport. */
    public static final class ListJobFunctionsOperation
            implements Query<ListJobFunctionsInput, ListJobFunctionsOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private static final String ENDPOINT_PREFIX =
                "https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/";
        private final int maxResponseBytes;

        /** Use the fixed provider endpoint with the manifest timeout and response limit. */
        public ListJobFunctionsOperation(
                CredentialProvider<SecretString> credentials, String organizationId, String mode) {
            this(
                    credentials,
                    organizationId,
                    mode,
                    new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.PUBLIC_HTTPS),
                    1 << 20);
        }

        /**
         * Bind the credential provider, caller-owned transport and maximum response size in bytes.
         */
        public ListJobFunctionsOperation(
                CredentialProvider<SecretString> credentials,
                String organizationId,
                String mode,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String selectedMode = mode == null || mode.isEmpty() ? "social" : mode;
            if (organizationId == null
                    || !organizationId.matches("[a-zA-Z0-9_-]{1,128}")
                    || !("social".equals(selectedMode) || "campus".equals(selectedMode)))
                throw new IllegalArgumentException("Invalid Moka organization or recruiting mode");
            endpoint = ENDPOINT_PREFIX + organizationId + "?mode=" + selectedMode;
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > (16 << 20)) {
                throw new IllegalArgumentException("Moka response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListJobFunctionsOutput> invoke(Call call, ListJobFunctionsInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid Moka API token");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", endpoint)
                            .header("Accept", "application/json")
                            .header(
                                    "Authorization",
                                    "Basic "
                                            + java.util.Base64.getEncoder()
                                                    .encodeToString(
                                                            (key.reveal() + ":")
                                                                    .getBytes(
                                                                            java.nio.charset
                                                                                    .StandardCharsets
                                                                                    .US_ASCII)))
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
                            "Moka rejected the request");
                byte[] body = response.readBoundedBody(maxResponseBytes);
                try {
                    return decodeFunctions(body);
                } catch (IOException invalidBody) {
                    return invalid();
                }
            } catch (BodyTooLargeException oversized) {
                return failed(
                        "invalidResponse",
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Moka response exceeded the configured limit");
            } catch (IOException interrupted) {
                return retry(FailureKind.AVAILABILITY, "Moka request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/moka/operation-definitions.json";
        try (InputStream input = JobFunctionDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListJobFunctionsOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Moka rate limited the request", delay);
        return retry(FailureKind.AVAILABILITY, "Moka is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 1024
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public WireItem[] jobs;
    }

    private static final class WireItem {
        public Long id;
        public String label;
        public Long parentId;
        public Long jobCount;
    }

    private static QueryAttempt<ListJobFunctionsOutput> decodeFunctions(byte[] body)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null) return invalid();
        WireItem[] items = page.jobs;
        if (items == null) return invalid();
        java.util.List<ListJobFunctionsOutput.JobFunction> values = new java.util.ArrayList<>();
        for (WireItem item : items) {
            if (item == null) return invalid();
            if (item.id == null
                    || item.id <= 0
                    || item.label == null
                    || item.label.isEmpty()
                    || item.jobCount == null
                    || item.jobCount < 0) return invalid();
            if (item.parentId == null) item.parentId = 0L;
            if (item.parentId < 0) return invalid();
            values.add(
                    new ListJobFunctionsOutput.JobFunction(
                            item.id, item.label, item.parentId, item.jobCount));
        }
        return QueryAttempt.branch("found", new ListJobFunctionsOutput(values), Receipt.empty());
    }

    private static QueryAttempt<ListJobFunctionsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid job function response");
    }

    private static QueryAttempt<ListJobFunctionsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListJobFunctionsOutput(java.util.Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListJobFunctionsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
