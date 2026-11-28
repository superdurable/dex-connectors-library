// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tripo;

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

/** The Tripo balance query and its operation-specific Dex Step configuration. */
public final class AccountBalance {
    private static final String CONNECTOR = "tripo";
    private static final String OPERATION = "getAccountBalance";
    private static final String BASE_URL = "https://openapi.tripo3d.com";
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
            throw new IllegalArgumentException("Tripo connection provider is invalid");
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
                            Duration.ofNanos(10000000000L), AddressPolicy.PUBLIC_HTTPS),
                    65536);
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
            if (!java.net.URI.create(origin).getRawPath().isEmpty())
                throw new IllegalArgumentException("Balance API base URL must be a host root");
            this.endpoint = origin + "/v3/account/balance";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Tripo response limit must be positive");
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
            if (key == null || !key.reveal().matches("[!-~]{1,512}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Tripo API Key is unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", endpoint)
                            .header("Authorization", "Bearer " + key.reveal())
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
                            "Tripo response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Tripo response was interrupted",
                            Duration.ZERO);
                }
                if (status != 200) return classifyStatus(response);
                return decodeBalance(body);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "Tripo balance request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/vast/tripo/operation-definitions.json";
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
            return retry(FailureKind.RATE_LIMIT, "Tripo rate limited the balance request", delay);
        if (status == 408 || status >= 500)
            return retry(FailureKind.AVAILABILITY, "Tripo API is temporarily unavailable", delay);
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (status == 401) kind = FailureKind.AUTHENTICATION;
        if (status == 403) kind = FailureKind.AUTHORIZATION;
        return failed(
                "providerRejected", kind, "Tripo rejected balance request with HTTP " + status);
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
        public Long code;
        public CreditBalances data;
    }

    private static final class CreditBalances {
        @JsonDeserialize(using = ProviderAmount.class)
        public String balance;

        @JsonDeserialize(using = ProviderAmount.class)
        public String frozen;
    }

    private static boolean validCredit(String value) {
        return value != null && value.length() <= 30 && value.matches("[0-9]+(?:\\.[0-9]{1,2})?");
    }

    private static QueryAttempt<GetAccountBalanceOutput> decodeBalance(byte[] body) {
        try {
            Response response = JSON.readValue(body, Response.class);
            if (response == null || response.code == null) return invalid();
            long code = response.code;
            if (code == 1000 || code == 1001)
                return retry(
                        FailureKind.AVAILABILITY,
                        "Tripo service is temporarily unavailable",
                        Duration.ZERO);
            if (code == 1007 || code == 2000)
                return retry(
                        FailureKind.RATE_LIMIT, "Tripo rate limited the request", Duration.ZERO);
            if (code != 0) {
                FailureKind kind = FailureKind.PROVIDER_REJECTION;
                if (code == 2 || code == 1002) kind = FailureKind.AUTHENTICATION;
                if (code == 2010) kind = FailureKind.QUOTA_EXHAUSTED;
                return failed("providerRejected", kind, "Tripo rejected the account request");
            }
            CreditBalances balances = response.data;
            if (balances == null || !validCredit(balances.balance) || !validCredit(balances.frozen))
                return invalid();
            return QueryAttempt.branch(
                    "fetched",
                    new GetAccountBalanceOutput(balances.balance, balances.frozen),
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
                new GetAccountBalanceOutput("", ""),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetAccountBalanceOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
