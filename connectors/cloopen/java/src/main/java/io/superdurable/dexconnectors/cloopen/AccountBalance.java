// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.cloopen;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import java.util.Base64;
import java.util.Locale;
import java.util.Objects;
import java.util.function.Function;

/** The Cloopen balance query and its operation-specific Dex Step configuration. */
public final class AccountBalance {
    private static final String CONNECTOR = "cloopen";
    private static final String OPERATION = "getBalance";
    private static final String BASE_URL = "https://app.cloopen.com:8883";
    private static final ObjectMapper JSON =
            new ObjectMapper()
                    .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(MapperFeature.ALLOW_COERCION_OF_SCALARS)
                    .disable(
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT,
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES);
    private static final DateTimeFormatter TIMESTAMP =
            DateTimeFormatter.ofPattern("yyyyMMddHHmmss", Locale.ROOT).withZone(ZoneOffset.UTC);
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
            throw new IllegalArgumentException("Cloopen connection provider is invalid");
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

    /** The main account SID and private auth token for a Cloopen account. */
    public static final class Credentials {
        private final SecretString accountSid;
        private final SecretString authToken;

        /** Bind the two provider credentials used by each account query. */
        public Credentials(SecretString accountSid, SecretString authToken) {
            this.accountSid = Objects.requireNonNull(accountSid);
            this.authToken = Objects.requireNonNull(authToken);
        }
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetBalanceOperation
            implements Query<GetBalanceInput, GetBalanceOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final String endpoint;
        private final int maxResponseBytes;
        private final Clock clock;

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
            this(credentials, baseUrl, transport, maxResponseBytes, Clock.systemUTC());
        }

        GetBalanceOperation(
                CredentialProvider<Credentials> credentials,
                String baseUrl,
                ProviderHttpTransport transport,
                int maxResponseBytes,
                Clock clock) {
            this.clock = Objects.requireNonNull(clock);
            String origin =
                    ProviderHttp.validateBaseUrl(
                            baseUrl == null || baseUrl.isEmpty() ? BASE_URL : baseUrl);
            if (!java.net.URI.create(origin).getRawPath().isEmpty())
                throw new IllegalArgumentException("Balance API base URL must be a host root");
            this.endpoint = origin + "/2013-12-26/Accounts/";
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0) {
                throw new IllegalArgumentException("Cloopen response limit must be positive");
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
                    || !key.accountSid.reveal().matches("[A-Za-z0-9]{32}")
                    || !key.authToken.reveal().matches("[!-~]{32}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Cloopen account SID or auth token is unavailable or invalid");
            }
            String accountSid = key.accountSid.reveal();
            String timestamp = TIMESTAMP.format(clock.instant());
            String signature = signature(accountSid, key.authToken.reveal(), timestamp);
            String authorization =
                    Base64.getEncoder()
                            .encodeToString(
                                    (accountSid + ":" + timestamp)
                                            .getBytes(StandardCharsets.US_ASCII));
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET", endpoint + accountSid + "/AccountInfo?sig=" + signature)
                            .header("Authorization", authorization)
                            .header("Accept", "application/json")
                            .header("Content-Type", "application/json;charset=utf-8")
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
                            "Cloopen response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Cloopen response was interrupted",
                            Duration.ZERO);
                }
                if (status != 200) return classifyStatus(response);
                return decodeBalance(body);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "Cloopen balance request failed", Duration.ZERO);
            }
        }
    }

    private static String signature(String accountSid, String token, String timestamp) {
        try {
            MessageDigest digest = MessageDigest.getInstance("MD5");
            digest.update(accountSid.getBytes(StandardCharsets.US_ASCII));
            digest.update(token.getBytes(StandardCharsets.US_ASCII));
            byte[] hash = digest.digest(timestamp.getBytes(StandardCharsets.US_ASCII));
            StringBuilder hex = new StringBuilder(32);
            for (byte octet : hash) hex.append(String.format(Locale.ROOT, "%02X", octet));
            return hex.toString();
        } catch (NoSuchAlgorithmException impossible) {
            throw new IllegalStateException("MD5 unavailable", impossible);
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/cloopen/operation-definitions.json";
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
            return retry(FailureKind.RATE_LIMIT, "Cloopen rate limited the balance request", delay);
        if (status == 408 || status >= 500)
            return retry(FailureKind.AVAILABILITY, "Cloopen API is temporarily unavailable", delay);
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (status == 401) kind = FailureKind.AUTHENTICATION;
        if (status == 403) kind = FailureKind.AUTHENTICATION;
        if (status == 400) kind = FailureKind.VALIDATION;
        return failed(
                "providerRejected", kind, "Cloopen rejected balance request with HTTP " + status);
    }

    private static final class Response {
        public JsonNode statusCode;
        public AccountInfo account;
    }

    private static final class AccountInfo {
        public JsonNode balance;
    }

    private static QueryAttempt<GetBalanceOutput> decodeBalance(byte[] body) {
        try {
            Response response = JSON.readValue(body, Response.class);
            if (response == null || response.statusCode == null || !response.statusCode.isTextual())
                return invalid();
            JsonNode balance = response.account == null ? null : response.account.balance;
            if (balance != null && !balance.isNull() && !balance.isTextual()) return invalid();
            String status = response.statusCode.textValue();
            if (status.isEmpty()) return invalid();
            if (status.equals("111111"))
                return retry(
                        FailureKind.RATE_LIMIT, "Cloopen concurrency limit reached", Duration.ZERO);
            if (status.startsWith("1110"))
                return retry(
                        FailureKind.AVAILABILITY,
                        "Cloopen reported an internal failure",
                        Duration.ZERO);
            if (!status.equals("000000"))
                return failed(
                        "providerRejected",
                        FailureKind.PROVIDER_REJECTION,
                        "Cloopen rejected the account lookup");
            if (balance == null || !balance.isTextual()) return invalid();
            String value = balance.textValue();
            if (value.length() > 64 || !value.matches("(0|[1-9][0-9]*)(\\.[0-9]+)?"))
                return invalid();
            return QueryAttempt.branch("fetched", new GetBalanceOutput(value), Receipt.empty());
        } catch (IOException error) {
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
                new GetBalanceOutput(""),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetBalanceOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
