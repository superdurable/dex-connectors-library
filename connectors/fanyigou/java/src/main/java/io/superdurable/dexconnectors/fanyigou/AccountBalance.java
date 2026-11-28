// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.fanyigou;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.core.*;
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

/** The Fanyigou balance query and its operation-specific Dex Step configuration. */
public final class AccountBalance {
    private static final String CONNECTOR = "fanyigou";
    private static final String OPERATION = "getAccountBalance";
    private static final String BASE_URL = "https://www.fanyigou.com";
    private static final ObjectMapper JSON =
            new ObjectMapper()
                    .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
                    .disable(
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT,
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private static final java.security.SecureRandom RANDOM = new java.security.SecureRandom();

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
            throw new IllegalArgumentException("Fanyigou connection provider is invalid");
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

    /** The application ID and private secret code for a Fanyigou account. */
    public static final class Credentials {
        private final SecretString appId;
        private final SecretString privateKey;

        /** Bind the two provider credentials used by each account query. */
        public Credentials(SecretString appId, SecretString privateKey) {
            this.appId = Objects.requireNonNull(appId);
            this.privateKey = Objects.requireNonNull(privateKey);
        }
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetAccountBalanceOperation
            implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final java.util.function.Supplier<byte[]> nonceSource;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetAccountBalanceOperation(CredentialProvider<Credentials> credentials) {
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
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(
                    credentials,
                    transport,
                    maxResponseBytes,
                    () -> {
                        byte[] bytes = new byte[16];
                        RANDOM.nextBytes(bytes);
                        return bytes;
                    });
        }

        GetAccountBalanceOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                java.util.function.Supplier<byte[]> nonceSource) {
            this.nonceSource = Objects.requireNonNull(nonceSource);
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Fanyigou response limit must be positive");
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
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !valid(key.appId.reveal()) || !valid(key.privateKey.reveal())) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Fanyigou account or privateKey is unavailable or invalid");
            }
            byte[] bytes;
            try {
                bytes = nonceSource.get();
            } catch (RuntimeException failure) {
                bytes = null;
            }
            if (bytes == null || bytes.length != 16)
                return retry(
                        FailureKind.AVAILABILITY,
                        "Fanyigou request nonce is unavailable",
                        Duration.ZERO);
            StringBuilder nonce = new StringBuilder(32);
            for (byte value : bytes) {
                nonce.append(Character.forDigit((value >>> 4) & 15, 16))
                        .append(Character.forDigit(value & 15, 16));
            }
            String form;
            try {
                form =
                        "appid="
                                + java.net.URLEncoder.encode(key.appId.reveal(), "UTF-8")
                                + "&nonce_str="
                                + nonce
                                + "&type=1";
            } catch (java.io.UnsupportedEncodingException impossible) {
                throw new AssertionError(impossible);
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "POST", BASE_URL + "/TranslateApi/api/getAccount")
                            .header("Accept", "application/json")
                            .header(
                                    "Content-Type",
                                    "application/x-www-form-urlencoded; charset=UTF-8")
                            .header("Authorization", "Bearer " + key.privateKey.reveal())
                            .body(form.getBytes(java.nio.charset.StandardCharsets.UTF_8))
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                if (status != 200) {
                    return classifyStatus(response);
                }
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Fanyigou response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Fanyigou response was interrupted",
                            Duration.ZERO);
                }

                return decodeBalance(
                        body,
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now()));
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "Fanyigou balance request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/fanyigou/operation-definitions.json";
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
            return retry(
                    FailureKind.RATE_LIMIT, "Fanyigou rate limited the balance request", delay);
        if (status == 408 || status >= 500)
            return retry(
                    FailureKind.AVAILABILITY, "Fanyigou API is temporarily unavailable", delay);
        return failed(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                "Fanyigou rejected balance request with HTTP " + status);
    }

    private static boolean valid(String value) {
        if (value.isEmpty()
                || value.getBytes(java.nio.charset.StandardCharsets.UTF_8).length > 1024)
            return false;
        int first = value.codePointAt(0), last = value.codePointBefore(value.length());
        if (space(first) || space(last)) return false;
        return value.chars().noneMatch(c -> c < 32 || c == 127);
    }

    private static boolean space(int c) {
        return Character.isWhitespace(c) || Character.isSpaceChar(c) || c == 0x85;
    }

    private static final class Response {
        public Long code;
        public Resources data;
    }

    private static final class Resources {
        public Long deductType;
        public Long balance;
    }

    private static QueryAttempt<GetAccountBalanceOutput> decodeBalance(
            byte[] body, Duration delay) {
        try {
            Response value = JSON.readValue(body, Response.class);
            if (value == null || value.code == null) return invalid();
            long code = value.code;
            if (code == 102 || code == 105)
                return retry(
                        FailureKind.AVAILABILITY, "Fanyigou account service is unavailable", delay);
            if (code == 108)
                return retry(
                        FailureKind.RATE_LIMIT, "Fanyigou account request was rate limited", delay);
            if (code != 100)
                return failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "Fanyigou rejected the account request");
            Resources data = value.data;
            if (data == null
                    || data.deductType == null
                    || (data.deductType != 0 && data.deductType != 1)
                    || data.balance == null
                    || data.balance < 0) return invalid();
            return QueryAttempt.branch(
                    "found",
                    new GetAccountBalanceOutput(data.deductType.intValue(), data.balance),
                    Receipt.empty());
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
                new GetAccountBalanceOutput(0, 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetAccountBalanceOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
