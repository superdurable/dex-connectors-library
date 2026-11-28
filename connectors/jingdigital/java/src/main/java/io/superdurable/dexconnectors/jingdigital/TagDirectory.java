// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.jingdigital;

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

/** The JINGdigital tag query and its operation-specific Dex Step configuration. */
public final class TagDirectory {
    private static final String CONNECTOR = "jingdigital";
    private static final String OPERATION = "listTags";
    private static final String BASE_URL = "https://jingopenapi.jingsocial.com";
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

    private TagDirectory() {}

    /** Select one page of tags. */
    public static final class ListTagsInput {
        /** One-based tag page. Zero selects the first page. */
        public final long page;

        /** Page size from 1 through 100. Zero selects 20. */
        public final long pageSize;

        /** Select the tag page to request. */
        @JsonCreator
        public ListTagsInput(
                @JsonProperty("page") long page, @JsonProperty("pageSize") long pageSize) {
            this.page = page;
            this.pageSize = pageSize;
        }
    }

    /** JINGdigital application credentials resolved at the start of each query attempt. */
    public static final class Credentials {
        /** Provider-issued client ID. */
        public final SecretString appId;

        /** Secret paired with the client ID. */
        public final SecretString appSecret;

        /** Bind a client ID and its paired secret. */
        public Credentials(SecretString appId, SecretString appSecret) {
            this.appId = Objects.requireNonNull(appId);
            this.appSecret = Objects.requireNonNull(appSecret);
        }
    }

    /** Concrete Dex Step input for a tag result. */
    public static final class ListTagsResult extends QueryResult<ListTagsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListTagsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListTagsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the tag query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListTagsInput, ListTagsOutput, ListTagsResult>
                    newListTagsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListTagsInput, ListTagsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListTagsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("JINGdigital connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListTagsResult::new);
    }

    /** Load the connection's settings and bind its project credential provider. */
    public static ListTagsOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "jingdigital".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(() ->
                                new IllegalArgumentException("JINGdigital connection is missing"));
        if (!"jingdigital".equals(selected.getProvider()))
            throw new IllegalArgumentException("JINGdigital connection provider differs");

        long timeout = 10000000000L;
        String base = BASE_URL;
        int limit = 262144;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid JINGdigital timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid JINGdigital response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown JINGdigital configuration field");
            }
        }
        ProjectCredentialProvider<Credentials> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("jingdigital", connectionName),
                        material -> {
                            JsonNode id = material.get("app_id"),
                                    secret = material.get("app_secret");
                            if (material.size() != 2
                                    || id == null
                                    || !id.isTextual()
                                    || secret == null
                                    || !secret.isTextual()
                                    || !validKey(id.textValue())
                                    || !validKey(secret.textValue()))
                                throw new IllegalArgumentException(
                                        "Invalid JINGdigital client credentials");
                            return new Credentials(
                                    new SecretString(id.textValue()),
                                    new SecretString(secret.textValue()));
                        },
                        null);
        return new ListTagsOperation(
                provider,
                base,
                new ProviderHttpClient(Duration.ofNanos(timeout), addressPolicy(base)),
                limit);
    }

    private static AddressPolicy addressPolicy(String baseUrl) {
        return "http".equalsIgnoreCase(java.net.URI.create(baseUrl).getScheme())
                ? AddressPolicy.PRIVATE_NETWORK_HTTP
                : AddressPolicy.PUBLIC_HTTPS;
    }

    /** Reads one tag page using bounded responses and a redirect-safe transport. */
    public static final class ListTagsOperation implements Query<ListTagsInput, ListTagsOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;

        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListTagsOperation(CredentialProvider<Credentials> credentials) {
            this(
                    credentials,
                    BASE_URL,
                    new ProviderHttpClient(Duration.ofSeconds(10), addressPolicy(BASE_URL)),
                    262144);
        }

        private final String baseUrl;

        /** Bind the provider origin, transport and maximum response size in bytes. */
        public ListTagsOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.baseUrl =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            if (!this.baseUrl.equals(BASE_URL))
                throw new IllegalArgumentException("JINGdigital requires its production origin");
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0)
                throw new IllegalArgumentException("Response limit must be positive");
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListTagsOutput> invoke(Call call, ListTagsInput input)
                throws InterruptedException {
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.appId.reveal()) || !validKey(key.appSecret.reveal()))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "JINGdigital credentials are unavailable or invalid");
            long page = input.page == 0 ? 1 : input.page,
                    pageSize = input.pageSize == 0 ? 20 : input.pageSize;
            if (page < 1 || page > 1000000 || pageSize < 1 || pageSize > 100)
                return failed("defect", FailureKind.VALIDATION, "Invalid tag page or page size");
            try {
                java.util.Map<String, String> credentialsBody = new java.util.LinkedHashMap<>();
                credentialsBody.put("app_id", key.appId.reveal());
                credentialsBody.put("app_secret", key.appSecret.reveal());
                StageResponse authorization =
                        send(
                                ProviderHttpRequest.newBuilder("POST", baseUrl + "/v1/oauth2/token")
                                        .header("Accept", "application/json")
                                        .header("Content-Type", "application/json")
                                        .body(JSON.writeValueAsBytes(credentialsBody))
                                        .build());
                if (authorization.failure != null) return authorization.failure;
                AuthorizationResponse token =
                        JSON.readValue(authorization.body, AuthorizationResponse.class);
                if (token == null || token.code == null) return invalid();
                if (token.code != 0) return providerFailure(token.code);
                String accessToken = token.data == null ? null : token.data.access_token;
                if (accessToken == null || !validKey(accessToken)) return invalid();
                java.util.Map<String, Object> filter = new java.util.LinkedHashMap<>();
                filter.put("tag_ids", java.util.Collections.emptyList());
                filter.put("level", 1);
                filter.put("page", page);
                filter.put("perPage", pageSize);
                StageResponse tags =
                        send(
                                ProviderHttpRequest.newBuilder(
                                                "POST", baseUrl + "/v1/tag/select/list")
                                        .header("Accept", "application/json")
                                        .header("Content-Type", "application/json")
                                        .header("App_id", key.appId.reveal())
                                        .header("Access_token", accessToken)
                                        .body(JSON.writeValueAsBytes(filter))
                                        .build());
                if (tags.failure != null) return tags.failure;
                return decodeTags(tags.body, page, pageSize);
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
                                    (status == 401 || status == 403)
                                            ? FailureKind.AUTHENTICATION
                                            : FailureKind.PROVIDER_REJECTION,
                                    "JINGdigital rejected the request"));
                return new StageResponse(body, null);
            } catch (BodyTooLargeException error) {
                return new StageResponse(
                        null,
                        failed(
                                "invalidResponse",
                                FailureKind.RESPONSE_TOO_LARGE,
                                "JINGdigital response exceeded the configured limit"));
            } catch (IOException error) {
                return new StageResponse(
                        null,
                        retry(
                                FailureKind.AVAILABILITY,
                                "JINGdigital request failed",
                                Duration.ZERO));
            }
        }
    }

    private static final class StageResponse {
        final byte[] body;
        final QueryAttempt<ListTagsOutput> failure;

        StageResponse(byte[] body, QueryAttempt<ListTagsOutput> failure) {
            this.body = body;
            this.failure = failure;
        }
    }

    private static final class AuthorizationResponse {
        public Long code;
        public TokenData data;
    }

    private static final class TokenData {
        public String access_token;
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/jingdigital/operation-definitions.json";
        try (InputStream input = TagDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListTagsOutput> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "JINGdigital rate limited the tag request", delay);
        return retry(FailureKind.AVAILABILITY, "JINGdigital is temporarily unavailable", delay);
    }

    private static int length(String value) {
        return value.getBytes(java.nio.charset.StandardCharsets.UTF_8).length;
    }

    private static boolean validKey(String key) {
        return !key.isEmpty()
                && length(key) <= 2048
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static QueryAttempt<ListTagsOutput> providerFailure(long code) {
        if (code == 20000 || code == 40005)
            return retry(
                    FailureKind.AVAILABILITY, "JINGdigital temporarily unavailable", Duration.ZERO);
        if (code == 20002)
            return retry(
                    FailureKind.RATE_LIMIT, "JINGdigital rate limited the request", Duration.ZERO);
        FailureKind kind =
                code == 20001 || code == 40006
                        ? FailureKind.AUTHENTICATION
                        : FailureKind.PROVIDER_REJECTION;
        return failed("providerRejected", kind, "JINGdigital rejected the request");
    }

    private static final class DirectoryResponse {
        public Long code;
        public TagData data;
    }

    private static final class TagData {
        public Tag[] items;
        public Pagination _meta;
    }

    private static final class Pagination {
        public Long totalCount, currentPage, perPage;
    }

    private static final class Tag {
        public Long id, level;
        public String name;
    }

    private static QueryAttempt<ListTagsOutput> decodeTags(byte[] body, long page, long pageSize)
            throws IOException {
        DirectoryResponse response = JSON.readValue(body, DirectoryResponse.class);
        if (response == null || response.code == null) return invalid();
        if (response.code != 0) return providerFailure(response.code);
        if (response.data == null
                || response.data.items == null
                || response.data.items.length > pageSize
                || response.data._meta == null) return invalid();
        Pagination meta = response.data._meta;
        long total = meta.totalCount == null ? 0 : meta.totalCount;
        if (total < 0
                || meta.currentPage == null
                || meta.currentPage != page
                || meta.perPage == null
                || meta.perPage < 1) return invalid();
        java.util.List<ListTagsOutput.TagSummary> tags = new java.util.ArrayList<>();
        for (Tag tag : response.data.items) {
            if (tag == null
                    || tag.id == null
                    || tag.id <= 0
                    || tag.name == null
                    || tag.name.isEmpty()
                    || length(tag.name) > 512) return invalid();
            long level = tag.level == null ? 0 : tag.level;
            if (level < 0 || level > 10) return invalid();
            tags.add(new ListTagsOutput.TagSummary(tag.id, tag.name, level));
        }
        long nextPage = 0;
        if (page * meta.perPage < total) {
            if (tags.isEmpty() || page == 1000000) return invalid();
            nextPage = page + 1;
        }
        return QueryAttempt.branch(
                "listed", new ListTagsOutput(tags, total, nextPage), Receipt.empty());
    }

    private static QueryAttempt<ListTagsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid tag response");
    }

    private static QueryAttempt<ListTagsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListTagsOutput(java.util.Collections.emptyList(), 0, 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListTagsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
