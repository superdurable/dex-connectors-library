// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitee;

import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.cfg.*;
import com.fasterxml.jackson.databind.json.JsonMapper;
import com.fasterxml.jackson.databind.type.LogicalType;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.*;
import java.net.URI;
import java.time.*;
import java.util.*;
import java.util.regex.Pattern;

/** A bounded read of one Gitee repository using credentials resolved per call. */
public final class GetRepositoryOperation
        implements Query<GetRepositoryInput, GetRepositoryOutput> {
    private static final String BASE_URL = "https://gitee.com";
    private static final Pattern SEGMENT = Pattern.compile("[A-Za-z0-9._-]{1,128}");
    private static final ObjectMapper JSON =
            JsonMapper.builder()
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
                    .disable(
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES,
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT)
                    .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
                    .build();

    static {
        MutableCoercionConfig strings = JSON.coercionConfigFor(LogicalType.Textual);
        for (CoercionInputShape shape :
                Arrays.asList(
                        CoercionInputShape.Integer,
                        CoercionInputShape.Float,
                        CoercionInputShape.Boolean))
            strings.setCoercion(shape, CoercionAction.Fail);
    }

    private static final QueryDefinition DEFINITION = loadDefinition();
    private static final GetRepositoryOutput EMPTY =
            new GetRepositoryOutput(new RepositoryMetadata(0, "", false, "", "", "", "", 0, 0));
    private final String baseUrl;
    private final CredentialProvider<SecretString> credentials;
    private final ProviderHttpTransport transport;
    private final int maxResponseBytes;

    /**
     * Bind a token provider using the public API, a ten-second timeout and a 256 KiB response
     * limit.
     */
    public GetRepositoryOperation(CredentialProvider<SecretString> credentials) {
        this(
                credentials,
                BASE_URL,
                new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.PUBLIC_HTTPS),
                262144);
    }

    /**
     * Bind a validated API root, caller-owned HTTP transport and positive response limit in bytes.
     */
    public GetRepositoryOperation(
            CredentialProvider<SecretString> credentials,
            String baseUrl,
            ProviderHttpTransport transport,
            int maxResponseBytes) {
        this.credentials = Objects.requireNonNull(credentials);
        this.baseUrl = ProviderHttp.validateBaseUrl(baseUrl);
        this.transport = Objects.requireNonNull(transport);
        if (maxResponseBytes < 1)
            throw new IllegalArgumentException("Response limit must be positive");
        this.maxResponseBytes = maxResponseBytes;
    }

    /** Load the connection's settings and resolve its access token at each lookup. */
    public static GetRepositoryOperation fromProject(LoadedProject project, String connectionName) {
        ConnectionConfiguration selected =
                project.getConfiguration().getConnections().stream()
                        .filter(entry ->
                                "gitee".equals(entry.getConnectorId())
                                        && connectionName.equals(entry.getConnectionName()))
                        .findFirst()
                        .orElseThrow(
                                () -> new IllegalArgumentException("Gitee connection is missing"));
        if (!"gitee".equals(selected.getProvider()))
            throw new IllegalArgumentException("Gitee connection provider differs");
        String base = BASE_URL;
        long timeout = 10000000000L;
        int limit = 262144;
        Iterator<Map.Entry<String, JsonNode>> fields = selected.getConfiguration().fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            switch (field.getKey()) {
                case "baseUrl":
                    if (!value.isTextual())
                        throw new IllegalArgumentException("Invalid Gitee API URL setting");
                    if (!value.textValue().isEmpty()) base = value.textValue();
                    break;
                case "timeout":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber()
                            || !value.canConvertToLong()
                            || value.longValue() <= 0)
                        throw new IllegalArgumentException("Invalid Gitee timeout setting");
                    timeout = value.longValue();
                    break;
                case "maxResponseBytes":
                    if (value.isTextual() && value.textValue().isEmpty()) break;
                    if (!value.isIntegralNumber() || !value.canConvertToInt())
                        throw new IllegalArgumentException("Invalid Gitee response limit");
                    limit = value.intValue();
                    break;
                default:
                    throw new IllegalArgumentException("Unknown Gitee configuration field");
            }
        }
        AddressPolicy policy =
                "http".equalsIgnoreCase(URI.create(base).getScheme())
                        ? AddressPolicy.PRIVATE_NETWORK_HTTP
                        : AddressPolicy.PUBLIC_HTTPS;
        ProjectCredentialProvider<SecretString> provider =
                new ProjectCredentialProvider<>(
                        project.getConnections(),
                        new ConnectionKey("gitee", connectionName),
                        material -> {
                            JsonNode token = material.get("access_token");
                            if (material.size() != 1
                                    || token == null
                                    || !token.isTextual()
                                    || token.textValue().isEmpty())
                                throw new IllegalArgumentException(
                                        "Invalid repository credentials");
                            return new SecretString(token.textValue());
                        },
                        null);
        return new GetRepositoryOperation(
                provider, base, new ProviderHttpClient(Duration.ofNanos(timeout), policy), limit);
    }

    @Override
    public QueryDefinition definition() {
        return DEFINITION;
    }

    @Override
    public QueryAttempt<GetRepositoryOutput> invoke(Call call, GetRepositoryInput input)
            throws InterruptedException {
        if (input == null || !validSegment(input.owner) || !validSegment(input.repo))
            return failed("defect", FailureKind.VALIDATION, "Invalid repository path");
        String token;
        try {
            SecretString resolved = credentials.resolve(call);
            token = resolved == null ? "" : resolved.reveal();
        } catch (IOException | RuntimeException error) {
            return failed(
                    "defect",
                    FailureKind.AUTHENTICATION,
                    "Connection token is unavailable or invalid");
        }
        if (token.length() > 512 || !ProviderHttp.isHeaderSafeCredential(token))
            return failed(
                    "defect",
                    FailureKind.AUTHENTICATION,
                    "Connection token is unavailable or invalid");
        String url = baseUrl + "/api/v5/repos/" + input.owner + "/" + input.repo;
        try {
            url += "?access_token=" + java.net.URLEncoder.encode(token, "UTF-8");
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", url)
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status == 408 || status == 429 || status >= 500) {
                    return retry(
                            status == 429 ? FailureKind.RATE_LIMIT : FailureKind.AVAILABILITY,
                            "Gitee is temporarily unavailable",
                            ProviderHttp.parseRetryAfter(
                                    response.getHeaders().first("Retry-After"), Instant.now()));
                }
                if (status == 404)
                    return failed(
                            "notFound",
                            FailureKind.PROVIDER_REJECTION,
                            "Repository was not found or is inaccessible");
                if (status != 200) {
                    FailureKind kind;
                    switch (status) {
                        case 401:
                            kind = FailureKind.AUTHENTICATION;
                            break;
                        case 403:
                            kind = FailureKind.AUTHORIZATION;
                            break;
                        default:
                            kind = FailureKind.PROVIDER_REJECTION;
                    }
                    return failed("providerRejected", kind, "Repository lookup was rejected");
                }
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Repository response exceeded the configured limit");
                }

                try {
                    WireRepository record = JSON.readValue(body, WireRepository.class);
                    if (record == null
                            || record.id <= 0
                            || record.fullName == null
                            || record.fullName.isEmpty()
                            || record.name == null
                            || record.name.isEmpty()
                            || record.starsCount < 0
                            || record.forksCount < 0) return invalid();
                    return QueryAttempt.branch(
                            "found",
                            new GetRepositoryOutput(
                                    new RepositoryMetadata(
                                            record.id,
                                            record.fullName,
                                            record.isPrivate,
                                            record.defaultBranch,
                                            record.name,
                                            record.description,
                                            record.htmlUrl,
                                            record.starsCount,
                                            record.forksCount)),
                            Receipt.empty());
                } catch (IOException error) {
                    return invalid();
                }
            }
        } catch (IOException error) {
            return retry(FailureKind.AVAILABILITY, "Gitee request failed", Duration.ZERO);
        }
    }

    private static boolean validSegment(String value) {
        return SEGMENT.matcher(value).matches() && !value.equals(".") && !value.equals("..");
    }

    private static QueryDefinition loadDefinition() {
        try (InputStream source =
                GetRepositoryOperation.class.getResourceAsStream(
                        "/META-INF/dex-connectors/gitee/operation-definitions.json")) {
            QueryDefinition definition =
                    JSON.treeToValue(
                            JSON.readTree(Objects.requireNonNull(source)).required("getRepository"),
                            QueryDefinition.class);
            definition.validate();
            return definition;
        } catch (IOException error) {
            throw new IllegalStateException("Invalid Gitee query definition", error);
        }
    }

    private static final class WireRepository {
        @JsonProperty("id")
        public long id;

        @JsonProperty("full_name")
        public String fullName;

        @JsonProperty("private")
        public boolean isPrivate;

        @JsonProperty("default_branch")
        public String defaultBranch;

        @JsonProperty("name")
        public String name;

        @JsonProperty("description")
        public String description;

        @JsonProperty("html_url")
        public String htmlUrl;

        @JsonProperty("stargazers_count")
        public long starsCount;

        @JsonProperty("forks_count")
        public long forksCount;
    }

    private static QueryAttempt<GetRepositoryOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid repository metadata");
    }

    private static QueryAttempt<GetRepositoryOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                EMPTY,
                Receipt.empty(),
                new Failure(kind, "gitee", "getRepository", message));
    }

    private static QueryAttempt<GetRepositoryOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, "gitee", "getRepository", message), delay);
    }
}
