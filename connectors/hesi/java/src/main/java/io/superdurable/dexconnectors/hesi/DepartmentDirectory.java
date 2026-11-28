// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hesi;

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

/** The Hesi department query and its operation-specific Dex Step configuration. */
public final class DepartmentDirectory {
    private static final String CONNECTOR = "hesi";
    private static final String OPERATION = "listDepartments";
    private static final String BASE_URL = "https://app.ekuaibao.com";
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

    /** A zero-based department page. A count of zero requests 20 records. */
    public static final class ListDepartmentsInput {
        /** Zero-based offset, from 0 through 1,000,000. */
        public final long start;

        /** Page size from 1 through 100. Zero selects the default. */
        public final long count;

        /** Request the first page with its default size. */
        public ListDepartmentsInput() {
            this(0, 0);
        }

        /** Select the offset and requested page size. */
        @JsonCreator
        public ListDepartmentsInput(
                @JsonProperty("start") long start, @JsonProperty("count") long count) {
            this.start = start;
            this.count = count;
        }
    }

    /** Hesi application credentials resolved at the start of each query attempt. */
    public static final class Credentials {
        /** Provider-issued application key. */
        public final SecretString appKey;

        /** Secret paired with the application key. */
        public final SecretString appSecurity;

        /** Bind an application key and its paired secret. */
        public Credentials(SecretString appKey, SecretString appSecurity) {
            this.appKey = Objects.requireNonNull(appKey);
            this.appSecurity = Objects.requireNonNull(appSecurity);
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
            throw new IllegalArgumentException("Hesi connection provider is invalid");
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
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            if (!java.util.Arrays.asList(
                            "https://app.ekuaibao.com",
                            "https://dd2.hosecloud.com",
                            "https://wx2.ekuaibao.com",
                            "https://kdcloud2.ekuaibao.com",
                            "https://ddenterprise.ekuaibao.com",
                            "https://feishu.ekuaibao.com")
                    .contains(origin))
                throw new IllegalArgumentException("Hesi requires a documented platform origin");
            this.endpoint = origin;
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Hesi response limit must be positive");
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
            if (key == null
                    || !validKey(key.appKey.reveal())
                    || !validKey(key.appSecurity.reveal()))
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Hesi credentials are unavailable or invalid");
            long count = input.count == 0 ? 20 : input.count;
            if (input.start < 0 || input.start > 1000000 || count < 1 || count > 100)
                return failed("defect", FailureKind.VALIDATION, "Invalid Hesi department window");
            try {
                java.util.Map<String, String> credentialsBody = new java.util.LinkedHashMap<>();
                credentialsBody.put("appKey", key.appKey.reveal());
                credentialsBody.put("appSecurity", key.appSecurity.reveal());
                StageResponse authorization =
                        send(
                                ProviderHttpRequest.newBuilder(
                                                "POST",
                                                endpoint + "/api/openapi/v1/auth/getAccessToken")
                                        .header("Accept", "application/json")
                                        .header("Content-Type", "application/json")
                                        .body(JSON.writeValueAsBytes(credentialsBody))
                                        .build());
                if (authorization.failure != null) return authorization.failure;
                AuthorizationResponse token =
                        JSON.readValue(authorization.body, AuthorizationResponse.class);
                if (token == null
                        || token.value == null
                        || token.value.accessToken == null
                        || !validKey(token.value.accessToken)) return invalid();
                String url =
                        endpoint
                                + "/api/openapi/v1/departments?accessToken="
                                + java.net.URLEncoder.encode(token.value.accessToken, "UTF-8")
                                + "&count="
                                + count
                                + "&start="
                                + input.start;
                StageResponse page =
                        send(
                                ProviderHttpRequest.newBuilder("GET", url)
                                        .header("Accept", "application/json")
                                        .build());
                if (page.failure != null) return page.failure;
                return decodeDepartments(page.body, input.start, count);
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
                                    status == 401 || status == 403
                                            ? FailureKind.AUTHENTICATION
                                            : FailureKind.PROVIDER_REJECTION,
                                    "Hesi rejected the request"));
                return new StageResponse(body, null);
            } catch (BodyTooLargeException error) {
                return new StageResponse(
                        null,
                        failed(
                                "invalidResponse",
                                FailureKind.RESPONSE_TOO_LARGE,
                                "Hesi response exceeded the configured limit"));
            } catch (IOException error) {
                return new StageResponse(
                        null,
                        retry(FailureKind.AVAILABILITY, "Hesi request failed", Duration.ZERO));
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
        public Token value;
    }

    private static final class Token {
        public String accessToken;
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/hesi/operation-definitions.json";
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
            return retry(FailureKind.RATE_LIMIT, "Hesi rate limited the department request", delay);
        return retry(FailureKind.AVAILABILITY, "Hesi is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return key.length() > 0
                && key.length() <= 2048
                && key.chars().allMatch(c -> c >= 33 && c <= 126);
    }

    private static final class DirectoryResponse {
        public WireDepartment[] items;
        public long count;
    }

    private static final class WireDepartment {
        public String id;
        public String name;
        public String code;
        public Boolean active;
    }

    private static boolean validText(String value, int maxLength) {
        return value != null
                && !value.isEmpty()
                && value.getBytes(java.nio.charset.StandardCharsets.UTF_8).length <= maxLength;
    }

    private static QueryAttempt<ListDepartmentsOutput> decodeDepartments(
            byte[] body, long start, long count) throws IOException {
        DirectoryResponse page = JSON.readValue(body, DirectoryResponse.class);
        if (page == null || page.items == null || page.items.length > count || page.count < 0)
            return invalid();
        java.util.List<ListDepartmentsOutput.DepartmentSummary> departments =
                new java.util.ArrayList<>(page.items.length);
        for (WireDepartment item : page.items) {
            String code = item == null || item.code == null ? "" : item.code;
            if (item == null
                    || !validText(item.id, 256)
                    || !validText(item.name, 512)
                    || code.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 256
                    || item.active == null) return invalid();
            departments.add(
                    new ListDepartmentsOutput.DepartmentSummary(
                            item.id, item.name, code, item.active));
        }
        long end = start + departments.size();
        long next = 0;
        if (end < page.count) {
            if (departments.isEmpty() || end > 1000000) return invalid();
            next = end;
        }
        return QueryAttempt.branch(
                "listed",
                new ListDepartmentsOutput(departments, page.count, next),
                Receipt.empty());
    }

    private static QueryAttempt<ListDepartmentsOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid department response");
    }

    private static QueryAttempt<ListDepartmentsOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new ListDepartmentsOutput(java.util.Collections.emptyList(), 0, 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<ListDepartmentsOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
