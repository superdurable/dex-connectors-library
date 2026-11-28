// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sendcloud;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.core.*;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.annotation.JsonDeserialize;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.time.Duration;
import java.time.Instant;
import java.util.Objects;
import java.util.function.Function;

/** The SendCloud balance query and its operation-specific Dex Step configuration. */
public final class AccountBalance {
    private static final String CONNECTOR = "sendcloud";
    private static final String OPERATION = "getAccountBalance";
    private static final String BASE_URL = "https://api.sendcloud.net";
    private static final ObjectMapper JSON =
            new ObjectMapper()
                    .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
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
            throw new IllegalArgumentException("SendCloud connection provider is invalid");
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

    /** The API user and API key for a SendCloud account. */
    public static final class Credentials {
        private final SecretString apiUser;
        private final SecretString apiKey;

        /** Bind the two provider credentials used by each account query. */
        public Credentials(SecretString apiUser, SecretString apiKey) {
            this.apiUser = Objects.requireNonNull(apiUser);
            this.apiKey = Objects.requireNonNull(apiKey);
        }
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetAccountBalanceOperation
            implements Query<GetAccountBalanceInput, GetAccountBalanceOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetAccountBalanceOperation(CredentialProvider<Credentials> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(
                            Duration.ofNanos(10000000000L), AddressPolicy.PUBLIC_HTTPS),
                    262144);
        }

        /**
         * Bind a call-time credential source, configured transport, and response limit (positive
         * bytes).
         */
        public GetAccountBalanceOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(credentials, BASE_URL, transport, maxResponseBytes);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public GetAccountBalanceOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            if (!java.net.URI.create(origin).getRawPath().isEmpty())
                throw new IllegalArgumentException("Balance API base URL must be a host root");
            this.endpoint = origin + "/apiv2/userinfo/get";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("SendCloud response limit must be positive");
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
            if (key == null
                    || !key.apiUser.reveal().matches("[!-~]{1,512}")
                    || !key.apiKey.reveal().matches("[!-~]{1,512}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "SendCloud API user or API key is unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("POST", endpoint)
                            .header("Accept", "application/json")
                            .header("Content-Type", "application/x-www-form-urlencoded")
                            .body(
                                    ("apiKey="
                                                    + formValue(key.apiKey)
                                                    + "&apiUser="
                                                    + formValue(key.apiUser))
                                            .getBytes(java.nio.charset.StandardCharsets.UTF_8))
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
                            "SendCloud response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "SendCloud response was interrupted",
                            Duration.ZERO);
                }
                return decodeBalance(
                        body,
                        status,
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now()));
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "SendCloud balance request failed",
                        Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/sendcloud/operation-definitions.json";
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
                    FailureKind.RATE_LIMIT, "SendCloud rate limited the balance request", delay);
        return retry(FailureKind.AVAILABILITY, "SendCloud is temporarily unavailable", delay);
    }

    private static String formValue(SecretString credential) {
        try {
            return java.net.URLEncoder.encode(credential.reveal(), "UTF-8");
        } catch (java.io.UnsupportedEncodingException impossible) {
            throw new AssertionError(impossible);
        }
    }

    private static final java.util.regex.Pattern NUMBER =
            java.util.regex.Pattern.compile(
                    "-?(?:0|[1-9][0-9]*)(?:\\.[0-9]+)?(?:[eE][+-]?[0-9]+)?");

    private static final class ProviderAmount extends JsonDeserializer<String> {
        @Override
        public String deserialize(JsonParser parser, DeserializationContext context)
                throws IOException {
            if (!parser.currentToken().isNumeric()
                    && parser.currentToken() != JsonToken.VALUE_STRING)
                throw new IOException("Invalid balance value");
            String amount = parser.getText();
            if (!NUMBER.matcher(amount).matches()) throw new IOException("Invalid balance decimal");
            return amount;
        }
    }

    private static final class Response {
        public Boolean result;
        public Long statusCode;
        public AccountInfo info;
    }

    private static final class AccountInfo {
        @JsonProperty("avaliableBalance")
        @JsonDeserialize(using = ProviderAmount.class)
        public String availableBalance;

        @JsonDeserialize(using = ProviderAmount.class)
        public String balance;

        public Long quota;
        public Long todayUsedQuota;
    }

    private static QueryAttempt<GetAccountBalanceOutput> decodeBalance(
            byte[] body, int status, Duration delay) {
        try {
            Response response = JSON.readValue(body, Response.class);
            if (response == null || response.result == null || response.statusCode == null)
                return invalid(status);
            long code = response.statusCode;
            if (code == 50000)
                return retry(FailureKind.RATE_LIMIT, "SendCloud rate limited the request", delay);
            if (status != 200 || !response.result || code != 200) {
                FailureKind kind = FailureKind.PROVIDER_REJECTION;
                if (code == 40005) kind = FailureKind.AUTHENTICATION;
                if (code == 40018 || code == 40025 || code == 40026 || code == 6001)
                    kind = FailureKind.AUTHORIZATION;
                return failed(
                        "providerRejected", kind, "SendCloud rejected the account balance request");
            }
            AccountInfo info = response.info;
            if (info == null
                    || info.balance == null
                    || info.availableBalance == null
                    || info.quota == null
                    || info.quota < 0
                    || info.todayUsedQuota == null
                    || info.todayUsedQuota < 0
                    || !Double.isFinite(Double.parseDouble(info.balance))
                    || !Double.isFinite(Double.parseDouble(info.availableBalance)))
                return invalid(status);
            return QueryAttempt.branch(
                    "retrieved",
                    new GetAccountBalanceOutput(
                            info.availableBalance, info.balance, info.quota, info.todayUsedQuota),
                    Receipt.empty());
        } catch (IOException | NumberFormatException error) {
            return invalid(status);
        }
    }

    private static QueryAttempt<GetAccountBalanceOutput> invalid(int status) {
        return status == 200
                ? failed("invalidResponse", FailureKind.PROTOCOL, "Invalid balance response")
                : failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "SendCloud rejected the balance request");
    }

    private static QueryAttempt<GetAccountBalanceOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetAccountBalanceOutput("", "", 0, 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetAccountBalanceOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
