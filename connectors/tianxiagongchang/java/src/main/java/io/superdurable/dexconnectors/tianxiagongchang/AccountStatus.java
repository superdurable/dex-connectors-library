// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tianxiagongchang;

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

/** The TianxiaGongchang account query and its operation-specific Dex Step configuration. */
public final class AccountStatus {
    private static final String CONNECTOR = "tianxiagongchang";
    private static final String OPERATION = "getAccountStatus";
    private static final String ENDPOINT = "https://open.tianxiagongchang.com/open/v1/account";
    private static final ObjectMapper JSON =
            new ObjectMapper()
                    .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
                    .disable(
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT,
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private AccountStatus() {}

    /** The account query takes an empty JSON object. */
    public static final class GetAccountStatusInput {}

    /** Concrete Dex Step input for a balance result. */
    public static final class GetAccountStatusResult extends QueryResult<GetAccountStatusOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetAccountStatusResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") GetAccountStatusOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the account query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            GetAccountStatusInput,
                            GetAccountStatusOutput,
                            GetAccountStatusResult>
                    newGetAccountStatusStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetAccountStatusInput, GetAccountStatusOutput> operation,
                            ConnectionRef connection,
                            Function<I, GetAccountStatusInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("TianxiaGongchang connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetAccountStatusResult::new);
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetAccountStatusOperation
            implements Query<GetAccountStatusInput, GetAccountStatusOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetAccountStatusOperation(CredentialProvider<SecretString> credentials) {
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
        public GetAccountStatusOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException(
                        "TianxiaGongchang response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<GetAccountStatusOutput> invoke(Call call, GetAccountStatusInput input)
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
                        "TianxiaGongchang API Key is unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", ENDPOINT)
                            .header("Authorization", "Bearer " + key.reveal())
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                Duration delay =
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now());
                if (status == 429)
                    return retry(
                            FailureKind.RATE_LIMIT,
                            "Provider rate limited the account query",
                            delay);
                if (status == 408 || status >= 500)
                    return retry(
                            FailureKind.AVAILABILITY, "Provider is temporarily unavailable", delay);
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "TianxiaGongchang response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "TianxiaGongchang response was interrupted",
                            Duration.ZERO);
                }
                return decodeAccount(body, status, delay);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "TianxiaGongchang account request failed",
                        Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/tianxiagongchang/operation-definitions.json";
        try (InputStream input = AccountStatus.class.getResourceAsStream(resource)) {
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

    private static final class Response {
        public Long code;
        public Account data;

        @JsonProperty("credits_charged")
        public Long charged;

        @JsonProperty("credits_balance")
        public Long balance;
    }

    private static final class Account {
        @JsonProperty("app_id")
        public Long appId;

        @JsonProperty("frozen_credits")
        public Long frozen;

        @JsonProperty("is_sandbox")
        public Boolean sandbox;
    }

    private static QueryAttempt<GetAccountStatusOutput> decodeAccount(
            byte[] body, int status, Duration delay) {
        try {
            Response value = JSON.readValue(body, Response.class);
            if (value == null || value.code == null) return invalid();
            Account account = value.data;
            long code = value.code;
            if (code == 42900)
                return retry(
                        FailureKind.RATE_LIMIT, "Provider rate limited the account query", delay);
            if (code == 50000 || code == 50400)
                return retry(
                        FailureKind.AVAILABILITY, "Provider is temporarily unavailable", delay);
            if (code == 40100)
                return failed(
                        "providerRejected",
                        FailureKind.AUTHENTICATION,
                        "Provider rejected the API key");
            if (code == 40300 || code == 42901)
                return failed(
                        "providerRejected",
                        FailureKind.AUTHORIZATION,
                        "Provider denied account access");
            if (code != 0)
                return failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "Provider rejected the account query");
            if (status != 200 || account == null) return invalid();
            if (value.charged == null
                    || value.charged != 0
                    || value.balance == null
                    || value.balance < 0
                    || account.appId == null
                    || account.appId <= 0
                    || account.frozen == null
                    || account.frozen < 0
                    || account.sandbox == null) return invalid();
            return QueryAttempt.branch(
                    "retrieved",
                    new GetAccountStatusOutput(
                            account.appId, value.balance, account.frozen, account.sandbox),
                    Receipt.empty());
        } catch (IOException error) {
            return invalid();
        }
    }

    private static QueryAttempt<GetAccountStatusOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid account response");
    }

    private static QueryAttempt<GetAccountStatusOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetAccountStatusOutput(0, 0, 0, false),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetAccountStatusOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
