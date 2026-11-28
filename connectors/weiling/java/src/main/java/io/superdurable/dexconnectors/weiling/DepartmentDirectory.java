// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiling;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.time.Duration;
import java.time.Instant;
import java.util.Objects;
import java.util.function.Function;

/** The Weiling department query and its operation-specific Dex Step configuration. */
public final class DepartmentDirectory {
    private static final String CONNECTOR = "weiling";
    private static final String OPERATION = "listDepartments";
    private static final String BASE_URL = "https://openapi.weiling.cn";
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

    private DepartmentDirectory() {}

    /** The authorized department directory needs no caller-supplied identifiers. */
    public static final class ListDepartmentsInput {}

    /** Weiling application credentials resolved at the start of each query attempt. */
    public static final class Credentials {
        /** Provider-issued application key. */
        public final SecretString appId;

        /** Secret paired with the application key. */
        public final SecretString appSecret;

        /** Bind an application key and its paired secret. */
        public Credentials(SecretString appId, SecretString appSecret) {
            this.appId = Objects.requireNonNull(appId);
            this.appSecret = Objects.requireNonNull(appSecret);
        }
    }

    /** Concrete Dex Step input for a department result. */
    public static final class ListDepartmentsResult extends QueryResult<ListDepartmentsOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public ListDepartmentsResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") ListDepartmentsOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the department query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I, ListDepartmentsInput, ListDepartmentsOutput, ListDepartmentsResult>
                    newListDepartmentsStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<ListDepartmentsInput, ListDepartmentsOutput> operation,
                            ConnectionRef connection,
                            Function<I, ListDepartmentsInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Weiling connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                ListDepartmentsResult::new);
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class ListDepartmentsOperation
            implements Query<ListDepartmentsInput, ListDepartmentsOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final java.util.function.LongSupplier monotonicNanos;
        private byte[] credentialHash;
        private String cachedToken;
        private long refreshAt;

        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public ListDepartmentsOperation(CredentialProvider<Credentials> credentials) {
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
        public ListDepartmentsOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(credentials, BASE_URL, transport, maxResponseBytes);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public ListDepartmentsOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(credentials, baseUrl, transport, maxResponseBytes, System::nanoTime);
        }

        ListDepartmentsOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                java.util.function.LongSupplier monotonicNanos) {
            this.monotonicNanos = Objects.requireNonNull(monotonicNanos);
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            if (!BASE_URL.equals(origin))
                throw new IllegalArgumentException("Weiling requires its documented HTTPS origin");
            this.endpoint = origin;
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Weiling response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<ListDepartmentsOutput> invoke(Call call, ListDepartmentsInput input)
                throws InterruptedException {
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !validKey(key.appId.reveal()) || !validKey(key.appSecret.reveal()))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Weiling credentials are unavailable or invalid");
            try {
                TokenResult token = accessToken(key);
                if (token.failure != null) return token.failure;
                String url =
                        endpoint + "/openapi/department/list?access_token=" + encode(token.value);
                StageResponse page =
                        send(
                                ProviderHttpRequest.newBuilder("GET", url)
                                        .header("Accept", "application/json")
                                        .build());
                if (page.failure != null) return page.failure;
                return decodeDepartments(page.body);
            } catch (IOException error) {
                return invalid();
            }
        }

        private synchronized TokenResult accessToken(Credentials key)
                throws IOException, InterruptedException {
            byte[] hash = fingerprint(key);
            if (cachedToken != null
                    && java.util.Arrays.equals(hash, credentialHash)
                    && monotonicNanos.getAsLong() - refreshAt < 0)
                return new TokenResult(cachedToken, null);
            String url =
                    endpoint
                            + "/openapi/auth/access_token/get?app_id="
                            + encode(key.appId.reveal())
                            + "&app_secret="
                            + encode(key.appSecret.reveal());
            StageResponse response =
                    send(
                            ProviderHttpRequest.newBuilder("GET", url)
                                    .header("Accept", "application/json")
                                    .build());
            if (response.failure != null) return new TokenResult(null, response.failure);
            AuthorizationResponse auth = JSON.readValue(response.body, AuthorizationResponse.class);
            if (auth == null || auth.code == null) return new TokenResult(null, invalid());
            if (auth.code != 0) return new TokenResult(null, providerFailure(auth.code));
            if (auth.data == null
                    || auth.data.accessToken == null
                    || !validKey(auth.data.accessToken)
                    || auth.data.expiresIn < 180
                    || auth.data.expiresIn > 86400) return new TokenResult(null, invalid());
            credentialHash = hash;
            cachedToken = auth.data.accessToken;
            refreshAt =
                    monotonicNanos.getAsLong()
                            + Duration.ofSeconds(auth.data.expiresIn - 120).toNanos();
            return new TokenResult(cachedToken, null);
        }

        private static String encode(String value) throws java.io.UnsupportedEncodingException {
            return java.net.URLEncoder.encode(value, "UTF-8");
        }

        private static byte[] fingerprint(Credentials key) {
            try {
                java.security.MessageDigest digest =
                        java.security.MessageDigest.getInstance("SHA-256");
                digest.update(
                        key.appId.reveal().getBytes(java.nio.charset.StandardCharsets.US_ASCII));
                digest.update((byte) 0);
                return digest.digest(
                        key.appSecret
                                .reveal()
                                .getBytes(java.nio.charset.StandardCharsets.US_ASCII));
            } catch (java.security.NoSuchAlgorithmException error) {
                throw new IllegalStateException(error);
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
                                    status == 401 || status == 403
                                            ? FailureKind.AUTHENTICATION
                                            : FailureKind.PROVIDER_REJECTION,
                                    "Weiling rejected the request"));
                return new StageResponse(body, null);
            } catch (BodyTooLargeException error) {
                return new StageResponse(
                        null,
                        failed(
                                "invalidResponse",
                                FailureKind.RESPONSE_TOO_LARGE,
                                "Weiling response exceeded the configured limit"));
            } catch (IOException error) {
                return new StageResponse(
                        null,
                        retry(FailureKind.AVAILABILITY, "Weiling request failed", Duration.ZERO));
            }
        }
    }

    private static final class StageResponse {
        final byte[] body;
        final QueryAttempt<ListDepartmentsOutput> failure;

        StageResponse(byte[] body, QueryAttempt<ListDepartmentsOutput> failure) {
            this.body = body;
            this.failure = failure;
        }
    }

    private static final class AuthorizationResponse {
        public Long code;
        public Token data;
    }

    private static final class Token {
        @JsonProperty("access_token")
        public String accessToken;

        @JsonProperty("expires_in")
        public long expiresIn;
    }

    private static final class TokenResult {
        final String value;
        final QueryAttempt<ListDepartmentsOutput> failure;

        TokenResult(String value, QueryAttempt<ListDepartmentsOutput> failure) {
            this.value = value;
            this.failure = failure;
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/weiling/operation-definitions.json";
        try (InputStream input = DepartmentDirectory.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<ListDepartmentsOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(
                    FailureKind.RATE_LIMIT, "Weiling rate limited the department request", delay);
        return retry(FailureKind.AVAILABILITY, "Weiling is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 2048
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public Long code;
        public DirectoryData data;
    }

    private static final class DirectoryData {
        @JsonProperty("data_list")
        public WireDepartment[] departments;
    }

    private static final class WireDepartment {
        @JsonProperty("dept_id")
        public String id;

        public String name;

        @JsonProperty("parent_id")
        public String parentId;
    }

    private static boolean validText(String value, int maxLength) {
        return value != null
                && !value.isEmpty()
                && value.getBytes(java.nio.charset.StandardCharsets.UTF_8).length <= maxLength;
    }

    private static QueryAttempt<ListDepartmentsOutput> decodeDepartments(byte[] body)
            throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null || page.code == null) return invalid();
        if (page.code != 0) return providerFailure(page.code);
        if (page.data == null || page.data.departments == null) return invalid();
        java.util.List<ListDepartmentsOutput.DepartmentSummary> departments =
                new java.util.ArrayList<>(page.data.departments.length);
        for (WireDepartment row : page.data.departments) {
            if (row == null
                    || !validText(row.id, 256)
                    || !validText(row.name, 512)
                    || (row.parentId != null
                            && row.parentId.getBytes(java.nio.charset.StandardCharsets.UTF_8).length
                                    > 256)) return invalid();
            departments.add(
                    new ListDepartmentsOutput.DepartmentSummary(
                            row.id, row.name, row.parentId == null ? "" : row.parentId));
        }
        return QueryAttempt.branch(
                "listed", new ListDepartmentsOutput(departments), Receipt.empty());
    }

    private static QueryAttempt<ListDepartmentsOutput> providerFailure(long code) {
        if (code == 1009)
            return retry(FailureKind.RATE_LIMIT, "Weiling rate limited the request", Duration.ZERO);
        return failed(
                "providerRejected",
                code == 403 || code == 1007 || code == 1008
                        ? FailureKind.AUTHENTICATION
                        : FailureKind.PROVIDER_REJECTION,
                "Weiling rejected the request");
    }

    private static QueryAttempt<ListDepartmentsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid department response");
    }

    private static QueryAttempt<ListDepartmentsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListDepartmentsOutput(java.util.Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListDepartmentsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
