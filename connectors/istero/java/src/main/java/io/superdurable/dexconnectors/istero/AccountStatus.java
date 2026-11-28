// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.istero;

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

/** The IsTero account query and its operation-specific Dex Step configuration. */
public final class AccountStatus {
    private static final String CONNECTOR = "istero";
    private static final String OPERATION = "getAccountStatus";
    private static final String ENDPOINT =
            "https://api.istero.com/resource/v1/isas/mine/information";
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
            throw new IllegalArgumentException("IsTero connection provider is invalid");
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

    /** ISAS account identity paired with the call-time API key. */
    public static final class Credentials {
        private final String isid;
        private final SecretString apiKey;

        /** Bind a decimal ISAS account identifier and its private bearer key. */
        public Credentials(String isid, SecretString apiKey) {
            this.isid = Objects.requireNonNull(isid);
            this.apiKey = Objects.requireNonNull(apiKey);
        }
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetAccountStatusOperation
            implements Query<GetAccountStatusInput, GetAccountStatusOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetAccountStatusOperation(CredentialProvider<Credentials> credentials) {
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
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("IsTero response limit must be positive");
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
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null
                    || !key.isid.matches("[1-9][0-9]{0,17}")
                    || !key.apiKey.reveal().matches("[!-~]{1,512}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "IsTero API Key is unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", ENDPOINT + "?isid=" + key.isid)
                            .header("Authorization", "Bearer " + key.apiKey.reveal())
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
                            "IsTero response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "IsTero response was interrupted",
                            Duration.ZERO);
                }
                return decodeAccount(body, status, delay, key.isid);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "IsTero account request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/istero/operation-definitions.json";
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
    }

    private static final class Account {
        public Long isid;

        @JsonProperty("today_used")
        public Long used;

        @JsonProperty("today_remaining")
        public Long remaining;
    }

    private static QueryAttempt<GetAccountStatusOutput> decodeAccount(
            byte[] body, int status, Duration delay, String isid) {
        try {
            Response value = JSON.readValue(body, Response.class);
            if (value == null || value.code == null) return invalid();
            Account account = value.data;
            long code = value.code;
            if (code == 429)
                return retry(
                        FailureKind.RATE_LIMIT, "Provider rate limited the account query", delay);
            if (code == 500)
                return retry(
                        FailureKind.AVAILABILITY, "Provider is temporarily unavailable", delay);
            if (code == 403)
                return failed(
                        "providerRejected",
                        FailureKind.AUTHENTICATION,
                        "Provider rejected the API key");
            if (code == 502)
                return failed(
                        "providerRejected",
                        FailureKind.AUTHORIZATION,
                        "Provider denied account access");
            if (code != 200)
                return failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "Provider rejected the account query");
            if (status != 200 || account == null) return invalid();
            if (account.isid == null
                    || !isid.equals(account.isid.toString())
                    || account.used == null
                    || account.used < 0
                    || account.remaining == null
                    || account.remaining < 0) return invalid();
            return QueryAttempt.branch(
                    "retrieved",
                    new GetAccountStatusOutput(account.isid, account.used, account.remaining),
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
                new GetAccountStatusOutput(0, 0, 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetAccountStatusOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
