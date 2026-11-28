// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.apizero;

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

/** The ApiZero account query and its operation-specific Dex Step configuration. */
public final class AccountInfo {
    private static final String CONNECTOR = "apizero";
    private static final String OPERATION = "getAccountInfo";
    private static final String ENDPOINT = "https://v1.apizero.cn/api/user-info";
    private static final ObjectMapper JSON =
            new ObjectMapper()
                    .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
                    .disable(
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT,
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private AccountInfo() {}

    /** The account query takes an empty JSON object. */
    public static final class GetAccountInfoInput {}

    /** Concrete Dex Step input for a balance result. */
    public static final class GetAccountInfoResult extends QueryResult<GetAccountInfoOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetAccountInfoResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") GetAccountInfoOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the account query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I, GetAccountInfoInput, GetAccountInfoOutput, GetAccountInfoResult>
                    newGetAccountInfoStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetAccountInfoInput, GetAccountInfoOutput> operation,
                            ConnectionRef connection,
                            Function<I, GetAccountInfoInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("ApiZero connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetAccountInfoResult::new);
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetAccountInfoOperation
            implements Query<GetAccountInfoInput, GetAccountInfoOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetAccountInfoOperation(CredentialProvider<SecretString> credentials) {
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
        public GetAccountInfoOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("ApiZero response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<GetAccountInfoOutput> invoke(Call call, GetAccountInfoInput input)
                throws InterruptedException {
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null
                    || !key.reveal().startsWith("sk_live_")
                    || !key.reveal().matches("[!-~]{1,1024}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "ApiZero API Key is unavailable or invalid");
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
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "ApiZero response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "ApiZero response was interrupted",
                            Duration.ZERO);
                }
                return decodeAccount(body, status, delay);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "ApiZero account request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/apizero/operation-definitions.json";
        try (InputStream input = AccountInfo.class.getResourceAsStream(resource)) {
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
        public JsonNode scope;
        public Wallet balance;
        public Key key;
    }

    private static final class Wallet {
        public JsonNode yuan;
    }

    private static final class Key {
        @JsonProperty("quota_remaining")
        public Long quota;
    }

    private static QueryAttempt<GetAccountInfoOutput> decodeAccount(
            byte[] body, int status, Duration delay) {
        try {
            Response value = JSON.readValue(body, Response.class);
            if (value == null || value.code == null) return malformedResponse(status, delay);
            Account account = value.data;
            if (account != null
                    && ((!isOptionalText(account.scope))
                            || (account.balance != null && !isOptionalText(account.balance.yuan))))
                return malformedResponse(status, delay);
            long code = value.code;
            if (code == 4029)
                return retry(
                        FailureKind.RATE_LIMIT, "Provider rate limited the account query", delay);
            if (code == 5000 || code == 5020 || code == 5021 || code == 5030)
                return retry(
                        FailureKind.AVAILABILITY, "Provider is temporarily unavailable", delay);
            if (code == 4011 || code == 4015)
                return failed(
                        "providerRejected",
                        FailureKind.AUTHENTICATION,
                        "Provider rejected the API key");
            if (code == 4013 || code == 4014)
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
            String scope = account.scope == null ? "" : account.scope.asText("");
            if ("account".equals(scope)
                    && account.balance != null
                    && account.balance.yuan != null) {
                String yuan = account.balance.yuan.textValue();
                if (yuan != null && yuan.length() <= 32 && yuan.matches("[0-9]+(?:\\.[0-9]+)?"))
                    return QueryAttempt.branch(
                            "found", new GetAccountInfoOutput(scope, yuan, null), Receipt.empty());
            }
            if ("key".equals(scope)
                    && account.key != null
                    && account.key.quota != null
                    && account.key.quota >= 0)
                return QueryAttempt.branch(
                        "found",
                        new GetAccountInfoOutput(scope, null, account.key.quota),
                        Receipt.empty());
            return invalid();
        } catch (IOException error) {
            return malformedResponse(status, delay);
        }
    }

    private static boolean isOptionalText(JsonNode value) {
        return value == null || value.isNull() || value.isTextual();
    }

    private static QueryAttempt<GetAccountInfoOutput> malformedResponse(
            int status, Duration delay) {
        if (status == 408 || status >= 500)
            return retry(FailureKind.AVAILABILITY, "Provider is temporarily unavailable", delay);
        if (status == 200) return invalid();
        FailureKind kind =
                status == 401
                        ? FailureKind.AUTHENTICATION
                        : status == 403
                                ? FailureKind.AUTHORIZATION
                                : FailureKind.PROVIDER_REJECTION;
        return failed("providerRejected", kind, "Provider rejected the account query");
    }

    private static QueryAttempt<GetAccountInfoOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid account response");
    }

    private static QueryAttempt<GetAccountInfoOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetAccountInfoOutput("", null, null),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetAccountInfoOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
