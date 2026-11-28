// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zadig;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;

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

/** The Zadig project query and its operation-specific Dex Step configuration. */
public final class ProjectDirectory {
    private static final String CONNECTOR = "zadig";
    private static final String OPERATION = "listProjects";
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

    private ProjectDirectory() {}

    /** Select one page of projects. */
    public static final class ListProjectsInput {
        /** One-based page number up to 1,000,000. Zero selects the first page. */
        public final long pageNum;

        /** Page size from 1 through 100. Zero selects 20. */
        public final long pageSize;

        /** Select the project page to request. */
        @JsonCreator
        public ListProjectsInput(
                @JsonProperty("pageNum") long pageNum, @JsonProperty("pageSize") long pageSize) {
            this.pageNum = pageNum;
            this.pageSize = pageSize;
        }
    }

    /** Concrete Dex Step input for a project result. */
    public static final class ListProjectsResult extends QueryResult<ListProjectsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListProjectsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListProjectsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the project query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListProjectsInput, ListProjectsOutput, ListProjectsResult>
                    newListProjectsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListProjectsInput, ListProjectsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListProjectsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Zadig connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListProjectsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListProjectsOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "zadig".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("Zadig connection is missing"));
        if (!"zadig".equals(selected.getProvider()))
            throw new IllegalArgumentException("Zadig connection provider differs");
        String base = "";
        long timeout = 10000000000L;
        int limit = 2 << 20;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Zadig API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Zadig timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Zadig response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Zadig configuration field");
            }
        }
        if (base.isEmpty())
            throw new IllegalArgumentException("Zadig deployment origin is required");
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("zadig", connectionName),
                        material -> {
                            JsonNode token = material.get("api_key");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException("Invalid Zadig credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new ListProjectsOperation(
                provider, base, new ProviderHttpClient(Duration.ofNanos(timeout), policy), limit);
    }

    /** Reads one project page using bounded responses and a redirect-safe transport. */
    public static final class ListProjectsOperation
            implements Query<ListProjectsInput, ListProjectsOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the deployment origin and the timeout and byte limit from the manifest. */
        public ListProjectsOperation(CredentialProvider<SecretString> credentials, String baseUrl) {
            this(
                    credentials,
                    baseUrl,
                    new ProviderHttpClient(
                            Duration.ofSeconds(10),
                            java.net.URI.create(baseUrl).getScheme().equalsIgnoreCase("http")
                                    ? AddressPolicy.PRIVATE_NETWORK_HTTP
                                    : AddressPolicy.PUBLIC_HTTPS),
                    2097152);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public ListProjectsOperation(
                CredentialProvider<SecretString> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin = ProviderHttp.validateBaseUrl(baseUrl);
            if (!java.net.URI.create(origin).getPath().isEmpty())
                throw new IllegalArgumentException(
                        "Zadig requires a deployment origin without a path");
            this.endpoint = origin;
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Zadig response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListProjectsOutput> invoke(Call call, ListProjectsInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal()))
                return failed("defect", FailureKind.AUTHENTICATION, "Invalid Zadig API token");
            long pageNum = input.pageNum == 0 ? 1 : input.pageNum;
            long pageSize = input.pageSize == 0 ? 20 : input.pageSize;
            if (pageNum < 1 || pageNum > 1000000 || pageSize < 1 || pageSize > 100)
                return failed(
                        "defect", FailureKind.VALIDATION, "Invalid project page number or size");
            StageResponse page =
                    send(
                            ProviderHttpRequest.newBuilder(
                                            "GET",
                                            endpoint
                                                    + "/openapi/projects/project?pageNum="
                                                    + pageNum
                                                    + "&pageSize="
                                                    + pageSize)
                                    .header("Authorization", "Bearer " + key.reveal())
                                    .header("Accept", "application/json")
                                    .build());
            if (page.failure != null) return page.failure;
            try {
                return decodeProjects(page.body, pageSize);
            } catch (IOException error) {
                return invalid();
            }
        }

        private StageResponse send(ProviderHttpRequest request) throws InterruptedException {
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status == 429 || status == 408 || status >= 500)
                    return new StageResponse(null, classifyStatus(response));
                byte[] body = response.readBoundedBody(maxResponseBytes);
                if (status != 200)
                    return new StageResponse(
                            null,
                            failed(
                                    "providerRejected",
                                    status == 401
                                            ? FailureKind.AUTHENTICATION
                                            : status == 403
                                                    ? FailureKind.AUTHORIZATION
                                                    : FailureKind.PROVIDER_REJECTION,
                                    "Zadig rejected the request"));
                return new StageResponse(body, null);
            } catch (BodyTooLargeException error) {
                return new StageResponse(
                        null,
                        failed(
                                "invalidResponse",
                                FailureKind.RESPONSE_TOO_LARGE,
                                "Zadig response exceeded the configured limit"));
            } catch (IOException error) {
                return new StageResponse(
                        null,
                        retry(FailureKind.AVAILABILITY, "Zadig request failed", Duration.ZERO));
            }
        }
    }

    private static final class StageResponse {
        final byte[] body;
        final QueryAttempt<ListProjectsOutput> failure;

        StageResponse(byte[] body, QueryAttempt<ListProjectsOutput> failure) {
            this.body = body;
            this.failure = failure;
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/zadig/operation-definitions.json";
        try (InputStream input = ProjectDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListProjectsOutput> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Zadig rate limited the project request", delay);
        return retry(FailureKind.AVAILABILITY, "Zadig is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 512
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public Long total;
        public WireProject[] projects;
    }

    private static final class WireProject {
        public String project_key;
        public String project_name;
        public String deploy_type;
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

    private static QueryAttempt<ListProjectsOutput> decodeProjects(byte[] body, long pageSize)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null
                || page.total == null
                || page.total < 0
                || page.projects == null
                || page.projects.length > pageSize) return invalid();
        java.util.List<ListProjectsOutput.Project> projects =
                new java.util.ArrayList<>(page.projects.length);
        for (WireProject item : page.projects) {
            if (item == null
                    || item.project_key == null
                    || item.project_key.isEmpty()
                    || hasEdgeSpace(item.project_key)
                    || length(item.project_key) > 256
                    || item.project_name == null
                    || item.project_name.isEmpty()
                    || length(item.project_name) > 512) return invalid();
            String deployment = item.deploy_type == null ? "" : item.deploy_type;
            if (length(deployment) > 64) return invalid();
            projects.add(
                    new ListProjectsOutput.Project(
                            item.project_key, item.project_name, deployment));
        }
        return QueryAttempt.branch(
                "listed", new ListProjectsOutput(projects, page.total), Receipt.empty());
    }

    private static QueryAttempt<ListProjectsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid project response");
    }

    private static QueryAttempt<ListProjectsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListProjectsOutput(java.util.Collections.emptyList(), 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListProjectsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
