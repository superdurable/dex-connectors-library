// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiban;

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

/** The Weiban tag query and its operation-specific Dex Step configuration. */
public final class TagDirectory {
    private static final String CONNECTOR = "weiban";
    private static final String OPERATION = "listTags";
    private static final String BASE_URL = "https://open.weibanzhushou.com";
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

    /** Empty input for the tag query. */
    public static final class ListTagsInput {
        @com.fasterxml.jackson.annotation.JsonValue
        private java.util.Map<String, Object> emptyObject() {
            return java.util.Collections.emptyMap();
        }
    }

    /** Weiban application credentials resolved at the start of each query attempt. */
    public static final class Credentials {
        /** Provider-issued client ID. */
        public final String corpId;

        /** Secret paired with the client ID. */
        public final SecretString secret;

        /** Bind a client ID and its paired secret. */
        public Credentials(String corpId, SecretString secret) {
            this.corpId = Objects.requireNonNull(corpId);
            this.secret = Objects.requireNonNull(secret);
        }
    }

    /** Concrete Dex Step input for a tag result. */
    public static final class ListTagsResult extends QueryResult<TagsResult> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListTagsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") TagsResult value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the tag query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, ListTagsInput, TagsResult, ListTagsResult> newListTagsStep(
                    String stepType,
                    Class<I> inputType,
                    StepAnnotations annotations,
                    Query<ListTagsInput, TagsResult> operation,
                    ConnectionRef connection,
                    Function<I, ListTagsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Weiban connection provider is invalid");
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
                                "weiban".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("Weiban connection is missing"));
        if (!"weiban".equals(selected.getProvider()))
            throw new IllegalArgumentException("Weiban connection provider differs");

        long timeout = 15000000000L;
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
                        throw new IllegalArgumentException("Invalid Weiban timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Weiban response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Weiban configuration field");
            }
        }
        ProjectCredentialProvider<Credentials> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("weiban", connectionName),
                        material -> {
                            JsonNode id = material.get("corp_id"), secret = material.get("secret");
                            if (material.size() != 2
                                    || id == null
                                    || !id.isTextual()
                                    || secret == null
                                    || !secret.isTextual()
                                    || !validKey(id.textValue())
                                    || !validKey(secret.textValue()))
                                throw new IllegalArgumentException(
                                        "Invalid Weiban client credentials");
                            return new Credentials(
                                    id.textValue(), new SecretString(secret.textValue()));
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
    public static final class ListTagsOperation implements Query<ListTagsInput, TagsResult> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;

        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListTagsOperation(CredentialProvider<Credentials> credentials) {
            this(
                    credentials,
                    BASE_URL,
                    new ProviderHttpClient(Duration.ofSeconds(15), addressPolicy(BASE_URL)),
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
        public QueryAttempt<TagsResult> invoke(Call call, ListTagsInput input)
                throws InterruptedException {
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException | IllegalArgumentException error) {
                key = null;
            }
            if (key == null || !validKey(key.corpId) || !validKey(key.secret.reveal()))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Weiban credentials are unavailable or invalid");
            try {
                java.util.Map<String, String> credentialsBody = new java.util.LinkedHashMap<>();
                credentialsBody.put("corp_id", key.corpId);
                credentialsBody.put("secret", key.secret.reveal());
                StageResponse authorization =
                        send(
                                ProviderHttpRequest.newBuilder(
                                                "POST", baseUrl + "/open-api/access_token/get")
                                        .header("Accept", "application/json")
                                        .header("Content-Type", "application/json")
                                        .body(JSON.writeValueAsBytes(credentialsBody))
                                        .build());
                if (authorization.failure != null) return authorization.failure;
                AuthorizationResponse token =
                        JSON.readValue(authorization.body, AuthorizationResponse.class);
                if (token == null || token.errcode == null) return invalid();
                if (token.errcode != 0) return providerFailure(token.errcode, true);
                String accessToken = token.access_token;
                if (accessToken == null
                        || !validKey(accessToken)
                        || token.expires_in == null
                        || token.expires_in <= 0) return invalid();
                String query =
                        "?access_token="
                                + java.net.URLEncoder.encode(accessToken, "UTF-8")
                                        .replace("*", "%2A")
                                        .replace("%7E", "~")
                                + "&limit=10&offset=0";
                StageResponse tags =
                        send(
                                ProviderHttpRequest.newBuilder(
                                                "GET", baseUrl + "/open-api/tag/list" + query)
                                        .header("Accept", "application/json")
                                        .build());
                if (tags.failure != null) return tags.failure;
                return decodeTags(tags.body);
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
                                                    : (status == 400
                                                                    || status == 404
                                                                    || status == 412)
                                                            ? FailureKind.VALIDATION
                                                            : FailureKind.PROVIDER_REJECTION,
                                    "Weiban rejected the request"));
                return new StageResponse(body, null);
            } catch (BodyTooLargeException error) {
                return new StageResponse(
                        null,
                        failed(
                                "invalidResponse",
                                FailureKind.RESPONSE_TOO_LARGE,
                                "Weiban response exceeded the configured limit"));
            } catch (IOException error) {
                return new StageResponse(
                        null,
                        retry(FailureKind.AVAILABILITY, "Weiban request failed", Duration.ZERO));
            }
        }
    }

    private static final class StageResponse {
        final byte[] body;
        final QueryAttempt<TagsResult> failure;

        StageResponse(byte[] body, QueryAttempt<TagsResult> failure) {
            this.body = body;
            this.failure = failure;
        }
    }

    private static final class AuthorizationResponse {
        public Long errcode;
        public String access_token;
        public Long expires_in;
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/weiban/operation-definitions.json";
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

    private static QueryAttempt<TagsResult> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Weiban rate limited the tag request", delay);
        return retry(FailureKind.AVAILABILITY, "Weiban is temporarily unavailable", delay);
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

    private static boolean validKey(String key) {
        return !key.isEmpty() && length(key) <= 2048 && !hasEdgeSpace(key);
    }

    private static QueryAttempt<TagsResult> providerFailure(long code, boolean authorization) {
        if (code == 500 || code == 10051 || code == 10052 || code == 10053)
            return retry(FailureKind.AVAILABILITY, "Weiban temporarily unavailable", Duration.ZERO);
        FailureKind kind =
                authorization ? FailureKind.AUTHENTICATION : FailureKind.PROVIDER_REJECTION;
        return failed("providerRejected", kind, "Weiban rejected the request");
    }

    private static final class DirectoryResponse {
        public Long errcode, total;
        public Tag[] tag_list;
    }

    private static final class Tag {
        public String tag_id, tag_name, tag_group;
    }

    private static QueryAttempt<TagsResult> decodeTags(byte[] body) throws IOException {
        DirectoryResponse response = JSON.readValue(body, DirectoryResponse.class);
        if (response == null || response.errcode == null) return invalid();
        if (response.errcode != 0) return providerFailure(response.errcode, false);
        if (response.total == null
                || response.total < 0
                || response.tag_list == null
                || response.tag_list.length > 100
                || response.tag_list.length > response.total) return invalid();
        java.util.List<TagsResult.TagMetadata> tags = new java.util.ArrayList<>();
        for (Tag tag : response.tag_list) {
            if (tags.size() == 10) break;
            if (tag == null
                    || tag.tag_id == null
                    || tag.tag_id.isEmpty()
                    || length(tag.tag_id) > 128) return invalid();
            String name = tag.tag_name == null ? "" : tag.tag_name,
                    group = tag.tag_group == null ? "" : tag.tag_group;
            if (length(name) > 512 || length(group) > 512) return invalid();
            tags.add(new TagsResult.TagMetadata(tag.tag_id, name, group));
        }
        return QueryAttempt.branch("listed", new TagsResult(tags, response.total), Receipt.empty());
    }

    private static QueryAttempt<TagsResult> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid tag response");
    }

    private static QueryAttempt<TagsResult> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new TagsResult(java.util.Collections.emptyList(), 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<TagsResult> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
