// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.chuanglan;

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

/** The Chuanglan balance query and its operation-specific Dex Step configuration. */
public final class AccountBalance {
    private static final String CONNECTOR = "chuanglan";
    private static final String OPERATION = "getBalance";
    private static final String BASE_URL = "https://smssh1.253.com";
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
    public static final class GetBalanceInput {}

    /** Concrete Dex Step input for a balance result. */
    public static final class GetBalanceResult extends QueryResult<GetBalanceOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetBalanceResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") GetBalanceOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the balance query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<I, GetBalanceInput, GetBalanceOutput, GetBalanceResult>
                    newGetBalanceStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetBalanceInput, GetBalanceOutput> operation,
                            ConnectionRef connection,
                            Function<I, GetBalanceInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("Chuanglan connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetBalanceResult::new);
    }

    /** The application ID and private secret code for a Chuanglan account. */
    public static final class Credentials {
        private final SecretString account;
        private final SecretString password;

        /** Bind the two provider credentials used by each account query. */
        public Credentials(SecretString account, SecretString password) {
            this.account = Objects.requireNonNull(account);
            this.password = Objects.requireNonNull(password);
        }
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetBalanceOperation
            implements Query<GetBalanceInput, GetBalanceOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetBalanceOperation(CredentialProvider<Credentials> credentials) {
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
        public GetBalanceOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this(credentials, BASE_URL, transport, maxResponseBytes);
        }

        /** Use a configured provider base URL, transport and positive response size limit. */
        public GetBalanceOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            if (!java.net.URI.create(origin).getRawPath().isEmpty())
                throw new IllegalArgumentException("Balance API base URL must be a host root");
            this.endpoint = origin + "/msg/balance/json";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Chuanglan response limit must be positive");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<GetBalanceOutput> invoke(Call call, GetBalanceInput input)
                throws InterruptedException {
            Credentials key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null
                    || !key.account.reveal().matches("[!-~]{1,512}")
                    || !key.password.reveal().matches("[!-~]{1,512}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Chuanglan account or password is unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("POST", endpoint)
                            .header("Accept", "application/json")
                            .header("Content-Type", "application/json")
                            .body(
                                    JSON.createObjectNode()
                                            .put("account", key.account.reveal())
                                            .put("password", key.password.reveal())
                                            .toString()
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
                            "Chuanglan response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Chuanglan response was interrupted",
                            Duration.ZERO);
                }
                if (status != 200) return classifyStatus(response);
                return decodeBalance(body);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "Chuanglan balance request failed",
                        Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/chuanglan/operation-definitions.json";
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

    private static QueryAttempt<GetBalanceOutput> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(
                    FailureKind.RATE_LIMIT, "Chuanglan rate limited the balance request", delay);
        if (status == 408 || status >= 500)
            return retry(
                    FailureKind.AVAILABILITY, "Chuanglan API is temporarily unavailable", delay);
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (status == 401) kind = FailureKind.AUTHENTICATION;
        if (status == 403) kind = FailureKind.AUTHENTICATION;
        if (status == 400) kind = FailureKind.VALIDATION;
        return failed(
                "providerRejected", kind, "Chuanglan rejected balance request with HTTP " + status);
    }

    private static QueryAttempt<GetBalanceOutput> decodeBalance(byte[] body) {
        String code = null, balance = "";
        try (JsonParser parser = JSON.getFactory().createParser(body)) {
            if (parser.nextToken() != JsonToken.START_OBJECT) return invalid();
            while (parser.nextToken() == JsonToken.FIELD_NAME) {
                String field = parser.currentName();
                JsonToken token = parser.nextToken();
                if (field.equalsIgnoreCase("code")) {
                    long start = parser.getTokenLocation().getByteOffset();
                    parser.skipChildren();
                    parser.finishToken();
                    int end = (int) parser.getCurrentLocation().getByteOffset();
                    String raw =
                            new String(
                                    body,
                                    (int) start,
                                    end - (int) start,
                                    java.nio.charset.StandardCharsets.UTF_8);
                    code = raw.replaceAll("^\"+|\"+$", "");
                } else if (field.equalsIgnoreCase("balance")) {
                    if (token != JsonToken.VALUE_NULL) {
                        if (token != JsonToken.VALUE_STRING) return invalid();
                        balance = parser.getText();
                    }
                } else parser.skipChildren();
            }
            if (parser.currentToken() != JsonToken.END_OBJECT
                    || parser.nextToken() != null
                    || code == null) return invalid();
            if (!code.equals("0")) {
                if (!code.matches("[+-]?[0-9]+")) return invalid();
                Long.parseLong(code);
                return failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "Chuanglan rejected the balance lookup");
            }
            if (!balance.matches("[+-]?[0-9]+")) return invalid();
            long count = Long.parseLong(balance);
            if (count < 0) return invalid();
            return QueryAttempt.branch("fetched", new GetBalanceOutput(count), Receipt.empty());
        } catch (IOException | NumberFormatException error) {
            return invalid();
        }
    }

    private static QueryAttempt<GetBalanceOutput> invalid() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Invalid balance response");
    }

    private static QueryAttempt<GetBalanceOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetBalanceOutput(0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetBalanceOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
