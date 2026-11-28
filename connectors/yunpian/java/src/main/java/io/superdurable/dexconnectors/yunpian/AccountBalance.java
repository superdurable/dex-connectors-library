// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yunpian;

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

/** The Yunpian balance query and its operation-specific Dex Step configuration. */
public final class AccountBalance {
    private static final String CONNECTOR = "yunpian";
    private static final String OPERATION = "getAccountBalance";
    private static final String BASE_URL = "https://sms.yunpian.com";
    private static final ObjectMapper JSON =
            new ObjectMapper()
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
                    .disable(
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT,
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private AccountBalance() {}

    /** The balance query takes an empty JSON object. */
    public static final class GetAccountBalanceInput {}

    /** Concrete Dex Step input for a balance result. */
    public static final class GetAccountBalanceResult extends QueryResult<GetAccountBalanceOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetAccountBalanceResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") GetAccountBalanceOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the balance query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            GetAccountBalanceInput,
                            GetAccountBalanceOutput,
                            GetAccountBalanceResult>
                    newGetAccountBalanceStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetAccountBalanceInput, GetAccountBalanceOutput> operation,
                            ConnectionRef connection,
                            Function<I, GetAccountBalanceInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Yunpian connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetAccountBalanceResult::new);
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetAccountBalanceOperation
            implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetAccountBalanceOperation(CredentialProvider<SecretString> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(
                            Duration.ofNanos(15000000000L), AddressPolicy.PUBLIC_HTTPS),
                    1048576);
        }

        /**
         * Bind a call-time credential source, configured transport, and response limit (positive
         * bytes).
         */
        public GetAccountBalanceOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(credentials, BASE_URL, transport, maxResponseBytes);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public GetAccountBalanceOperation(
                CredentialProvider<SecretString> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            this.endpoint = origin + "/v2/user/get.json";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Yunpian response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<GetAccountBalanceOutput> invoke(Call call, GetAccountBalanceInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !validKey(key.reveal())) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Yunpian API Key is unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("POST", endpoint)
                            .header(
                                    "Content-Type",
                                    "application/x-www-form-urlencoded; charset=utf-8")
                            .body(encodedForm(key.reveal()))
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status == 408 || status == 429 || status >= 500) {
                    return classifyStatus(response);
                }
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Yunpian response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.TRANSPORT,
                            "Yunpian response was interrupted",
                            Duration.ZERO);
                }
                return decodeBalance(
                        body,
                        status,
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now()));
            } catch (IOException error) {
                return retry(
                        FailureKind.TRANSPORT, "Yunpian balance request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/yunpian/operation-definitions.json";
        try (InputStream input = AccountBalance.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<GetAccountBalanceOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Yunpian rate limited the balance request", delay);
        return retry(FailureKind.AVAILABILITY, "Yunpian is temporarily unavailable", delay);
    }

    private static boolean validKey(String key) {
        return !key.isEmpty()
                && !whiteSpace(key.codePointAt(0))
                && !whiteSpace(key.codePointBefore(key.length()));
    }

    private static boolean whiteSpace(int codePoint) {
        return codePoint == 0x85
                || Character.isSpaceChar(codePoint)
                || (codePoint >= 9 && codePoint <= 13);
    }

    private static byte[] encodedForm(String apiKey) {
        try {
            return ("apikey=" + java.net.URLEncoder.encode(apiKey, "UTF-8"))
                    .getBytes(java.nio.charset.StandardCharsets.UTF_8);
        } catch (java.io.UnsupportedEncodingException impossible) {
            throw new AssertionError(impossible);
        }
    }

    private static final class Response {
        public Long code;
        public JsonNode balance;
    }

    private static QueryAttempt<GetAccountBalanceOutput> decodeBalance(
            byte[] body, int status, Duration delay) {
        try {
            Response value = JSON.readValue(body, Response.class);
            long code = value == null || value.code == null ? 0 : value.code;
            if (code == -5)
                return retry(FailureKind.RATE_LIMIT, "Yunpian query frequency exceeded", delay);
            if (code == -50 || code == -51)
                return retry(
                        FailureKind.AVAILABILITY, "Yunpian reported a temporary failure", delay);
            if (code != 0 || status != 200)
                return failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "Yunpian rejected the balance lookup");
            if (value == null || value.balance == null || !value.balance.isNumber())
                return invalid();
            double balance = value.balance.doubleValue();
            if (!Double.isFinite(balance) || balance < 0) return invalid();
            return QueryAttempt.branch(
                    "read", new GetAccountBalanceOutput(balance), Receipt.empty());
        } catch (IOException error) {
            return invalid();
        }
    }

    private static QueryAttempt<GetAccountBalanceOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid balance response");
    }

    private static QueryAttempt<GetAccountBalanceOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetAccountBalanceOutput(0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetAccountBalanceOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
