// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.worktile;

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
import java.time.Duration;
import java.time.Instant;
import java.util.Iterator;
import java.util.Map;
import java.util.Objects;
import java.util.function.Function;

/** The Worktile project query and its operation-specific Dex Step configuration. */
public final class ProjectDirectory {
    private static final String CONNECTOR = "worktile";
    private static final String OPERATION = "listProjects";
    private static final String BASE_URL = "https://dev.worktile.com";
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
        /** Zero-based project page. */
        public final long pageIndex;

        /** Page size from 1 through 100. */
        public final long pageSize;

        /** Select the project page to request. */
        @JsonCreator
        public ListProjectsInput(
                @JsonProperty("pageIndex") long pageIndex,
                @JsonProperty("pageSize") long pageSize) {
            this.pageIndex = pageIndex;
            this.pageSize = pageSize;
        }
    }

    /** Worktile application credentials resolved at the start of each query attempt. */
    public static final class Credentials {
        /** Provider-issued client ID. */
        public final SecretString clientId;

        /** Secret paired with the client ID. */
        public final SecretString clientSecret;

        /** Bind a client ID and its paired secret. */
        public Credentials(SecretString clientId, SecretString clientSecret) {
            this.clientId = Objects.requireNonNull(clientId);
            this.clientSecret = Objects.requireNonNull(clientSecret);
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
            throw new IllegalArgumentException("Worktile connection provider is invalid");
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
                                "worktile".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("Worktile connection is missing"));
        if (!"worktile".equals(selected.getProvider()))
            throw new IllegalArgumentException("Worktile connection provider differs");

        long timeout = 10000000000L;
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
                        throw new IllegalArgumentException("Invalid Worktile timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Worktile response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Worktile configuration field");
            }
        }
        ProjectCredentialProvider<Credentials> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("worktile", connectionName),
                        material -> {
                            JsonNode id = material.get("client_id"),
                                    secret = material.get("client_secret");
                            if (material.size() != 2
                                    || id == null
                                    || !id.isTextual()
                                    || secret == null
                                    || !secret.isTextual()
                                    || !validKey(id.textValue())
                                    || !validKey(secret.textValue()))
                                throw new IllegalArgumentException(
                                        "Invalid Worktile client credentials");
                            return new Credentials(
                                    new SecretString(id.textValue()),
                                    new SecretString(secret.textValue()));
                        },
                        null);
        return new ListProjectsOperation(
                provider,
                new ProviderHttpClient(Duration.ofNanos(timeout), AddressPolicy.PUBLIC_HTTPS),
                limit);
    }

    /** Reads one project page using bounded responses and a redirect-safe transport. */
    public static final class ListProjectsOperation
            implements Query<ListProjectsInput, ListProjectsOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;

        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListProjectsOperation(CredentialProvider<Credentials> credentials) {
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
        public ListProjectsOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > (16 << 20)) {
                throw new IllegalArgumentException(
                        "Worktile response limit must be between 1 and 16777216 bytes");
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
            if (input.pageIndex < 0 || input.pageSize < 1 || input.pageSize > 100)
                return failed(
                        "defect", FailureKind.VALIDATION, "Invalid project page index or size");
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null
                    || !validKey(key.clientId.reveal())
                    || !validKey(key.clientSecret.reveal()))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Worktile credentials are unavailable or invalid");
            try {
                java.util.Map<String, String> credentialsBody = new java.util.LinkedHashMap<>();
                credentialsBody.put("client_id", key.clientId.reveal());
                credentialsBody.put("client_secret", key.clientSecret.reveal());
                StageResponse authorization =
                        send(
                                ProviderHttpRequest.newBuilder(
                                                "POST", BASE_URL + "/open-api/tenant-access-token")
                                        .header("Accept", "application/json")
                                        .header("Content-Type", "application/json")
                                        .body(JSON.writeValueAsBytes(credentialsBody))
                                        .build());
                if (authorization.failure != null) return authorization.failure;
                AuthorizationResponse token =
                        JSON.readValue(authorization.body, AuthorizationResponse.class);
                if (token == null
                        || token.tenant_access_token == null
                        || !validKey(token.tenant_access_token)
                        || token.expires_in == null
                        || token.expires_in <= 0) return invalid();
                String url =
                        BASE_URL
                                + "/open-api/mission/get-projects-by-page?access_token="
                                + java.net.URLEncoder.encode(token.tenant_access_token, "UTF-8")
                                + "&page_index="
                                + input.pageIndex
                                + "&page_size="
                                + input.pageSize;
                StageResponse page =
                        send(
                                ProviderHttpRequest.newBuilder("GET", url)
                                        .header("Accept", "application/json")
                                        .build());
                if (page.failure != null) return page.failure;
                return decodeProjects(page.body, input.pageIndex);
            } catch (IOException error) {
                return invalid();
            }
        }

        private StageResponse send(ProviderHttpRequest request) throws InterruptedException {
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status == 429 || status == 408 || status >= 500)
                    return new StageResponse(null, classifyStatus(response));
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
                                    "Worktile rejected the request"));
                return new StageResponse(response.readBoundedBody(maxResponseBytes), null);
            } catch (BodyTooLargeException error) {
                return new StageResponse(
                        null,
                        failed(
                                "invalidResponse",
                                FailureKind.RESPONSE_TOO_LARGE,
                                "Worktile response exceeded the configured limit"));
            } catch (IOException error) {
                return new StageResponse(
                        null,
                        retry(FailureKind.AVAILABILITY, "Worktile request failed", Duration.ZERO));
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

    private static final class AuthorizationResponse {
        public String tenant_access_token;
        public Long expires_in;
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/worktile/operation-definitions.json";
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
            return retry(
                    FailureKind.RATE_LIMIT, "Worktile rate limited the project request", delay);
        return retry(FailureKind.AVAILABILITY, "Worktile is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 1024
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public Long page_index;
        public Long page_size;
        public Boolean has_more;
        public WireProject[] items;
    }

    private static final class WireProject {
        public String _id;
        public String name;
        public String identifier;
        public Long visibility;
    }

    private static QueryAttempt<ListProjectsOutput> decodeProjects(byte[] body, long pageIndex)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null
                || page.page_index == null
                || page.page_index != pageIndex
                || page.page_size == null
                || page.page_size < 1
                || page.page_size > 100
                || page.has_more == null
                || page.items == null) return invalid();
        java.util.List<ListProjectsOutput.Project> projects =
                new java.util.ArrayList<>(page.items.length);
        for (WireProject item : page.items) {
            if (item == null
                    || item._id == null
                    || item._id.isEmpty()
                    || item.name == null
                    || item.name.isEmpty()
                    || item.visibility == null
                    || (item.visibility != 1 && item.visibility != 2)) return invalid();
            projects.add(
                    new ListProjectsOutput.Project(
                            item._id,
                            item.name,
                            item.identifier == null ? "" : item.identifier,
                            item.visibility));
        }
        return QueryAttempt.branch(
                "found",
                new ListProjectsOutput(projects, page.page_index, page.page_size, page.has_more),
                Receipt.empty());
    }

    private static QueryAttempt<ListProjectsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid project response");
    }

    private static QueryAttempt<ListProjectsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListProjectsOutput(java.util.Collections.emptyList(), 0, 0, false),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListProjectsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
