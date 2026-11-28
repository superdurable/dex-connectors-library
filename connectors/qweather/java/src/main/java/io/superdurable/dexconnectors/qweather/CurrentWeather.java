// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qweather;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.math.*;
import java.nio.charset.StandardCharsets;
import java.time.DateTimeException;
import java.time.Duration;
import java.time.Instant;
import java.time.LocalDate;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Objects;
import java.util.function.Function;
import java.util.regex.*;

/** The QWeather weather query and its operation-specific Dex Step configuration. */
public final class CurrentWeather {
    private static final String CONNECTOR = "qweather";
    private static final String OPERATION = "getCurrentWeather";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private CurrentWeather() {}

    /** Per-account QWeather credentials, resolved for each Execute attempt. */
    public static final class Credentials {
        /** Account host without scheme, port or path. */
        public final SecretString apiHost;

        /** Provider-issued API key. */
        public final SecretString apiKey;

        /** Bind the API key to its account host. */
        public Credentials(SecretString apiHost, SecretString apiKey) {
            this.apiHost = Objects.requireNonNull(apiHost);
            this.apiKey = Objects.requireNonNull(apiKey);
        }

        private boolean isValid() {
            String host = apiHost.reveal();
            if (host.length() > 253
                    || !host.endsWith(".qweatherapi.com")
                    || !apiKey.reveal().matches("[!-~]{1,1024}")) return false;
            for (String label : host.split("\\.", -1)) {
                if (!label.matches("[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?")) return false;
            }
            return true;
        }

        private boolean containsCredential(String value) {
            return value.contains(apiHost.reveal()) || value.contains(apiKey.reveal());
        }
    }

    /** Location to query in decimal degrees. */
    public static final class GetCurrentWeatherInput {
        /** Latitude, from -90 to 90. */
        public final double latitude;

        /** Longitude, from -180 to 180. */
        public final double longitude;

        /** Restore the weather fields from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherInput(
                @JsonProperty("latitude") double latitude,
                @JsonProperty("longitude") double longitude) {
            this.latitude = latitude;
            this.longitude = longitude;
        }
    }

    /** Current conditions and provider attributions. */
    public static final class GetCurrentWeatherOutput {
        /** Localized condition description. */
        public final String conditionText;

        /** Provider condition code. */
        public final String conditionCode;

        /** Measured temperature. */
        public final double temperatureValue;

        /** Unit of the temperature measurement. */
        public final String temperatureUnit;

        /** Observation timestamp with timezone. */
        public final String validTime;

        /** Attributions to display with the observation. */
        public final List<String> attributions;

        /** Restore the weather fields from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherOutput(
                @JsonProperty("conditionText") String conditionText,
                @JsonProperty("conditionCode") String conditionCode,
                @JsonProperty("temperatureValue") double temperatureValue,
                @JsonProperty("temperatureUnit") String temperatureUnit,
                @JsonProperty("validTime") String validTime,
                @JsonProperty("attributions") List<String> attributions) {
            this.conditionText = conditionText;
            this.conditionCode = conditionCode;
            this.temperatureValue = temperatureValue;
            this.temperatureUnit = temperatureUnit;
            this.validTime = validTime;
            this.attributions = attributions;
        }
    }

    /** Concrete Dex Step input for a weather result. */
    public static final class GetCurrentWeatherResult extends QueryResult<GetCurrentWeatherOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetCurrentWeatherResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") GetCurrentWeatherOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the weather query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            GetCurrentWeatherInput,
                            GetCurrentWeatherOutput,
                            GetCurrentWeatherResult>
                    newGetCurrentWeatherStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> operation,
                            ConnectionRef connection,
                            Function<I, GetCurrentWeatherInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("QWeather connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                GetCurrentWeatherResult::new);
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class GetCurrentWeatherOperation
            implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
        private final CredentialProvider<Credentials> credentials;
        private final ProviderHttpTransport transport;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetCurrentWeatherOperation(CredentialProvider<Credentials> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(Duration.ofSeconds(10), AddressPolicy.PUBLIC_HTTPS),
                    1 << 20);
        }

        /**
         * Bind a call-time credential source, configured transport, and response limit (one byte to
         * sixteen MiB).
         */
        public GetCurrentWeatherOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > 16 << 20) {
                throw new IllegalArgumentException(
                        "QWeather response limit must be between one byte and sixteen MiB");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<GetCurrentWeatherOutput> invoke(Call call, GetCurrentWeatherInput input)
                throws InterruptedException {
            if (input == null
                    || !(Math.abs(input.latitude) <= 90 && Math.abs(input.longitude) <= 180))
                return failed("defect", FailureKind.VALIDATION, "Latitude or longitude is invalid");
            Credentials account;
            try {
                account = credentials.resolve(call);
            } catch (IOException error) {
                account = null;
            }
            if (account == null || !account.isValid())
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "API Host or key is unavailable or invalid");
            String path =
                    "/weather/v1/current/"
                            + coordinate(input.latitude)
                            + "/"
                            + coordinate(input.longitude);
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET", "https://" + account.apiHost.reveal() + path)
                            .header("Accept", "application/json")
                            .header("X-Qw-Api-Key", account.apiKey.reveal())
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                Duration delay =
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now());
                if (status == 408 || status >= 500)
                    return retry(
                            FailureKind.AVAILABILITY, "QWeather is temporarily unavailable", delay);
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    if (status == 429) return rateLimit(delay);
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "QWeather response exceeded the configured limit");
                } catch (IOException error) {
                    if (status == 429) return rateLimit(delay);
                    return retry(
                            FailureKind.AVAILABILITY,
                            "QWeather response was interrupted",
                            Duration.ZERO);
                }
                if (status == 429) {
                    try {
                        JsonNode problem = lowercaseFields(JSON.readTree(body));
                        if (problem != null
                                && problem.path("error")
                                        .path("type")
                                        .asText("")
                                        .toLowerCase(java.util.Locale.ROOT)
                                        .contains("over-monthly-limit"))
                            return retry(
                                    FailureKind.QUOTA_EXHAUSTED,
                                    "QWeather monthly request limit was reached",
                                    delay);
                    } catch (IOException malformed) {
                        return rateLimit(delay);
                    }
                    return rateLimit(delay);
                }
                if (status != 200) {
                    FailureKind kind =
                            status == 401
                                    ? FailureKind.AUTHENTICATION
                                    : status == 403
                                            ? FailureKind.AUTHORIZATION
                                            : FailureKind.PROVIDER_REJECTION;
                    return failed(
                            "providerRejected",
                            kind,
                            "QWeather rejected the request with HTTP " + status);
                }
                return decodeWeather(body, account);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "QWeather weather request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/qweather/operation-definitions.json";
        try (InputStream input = CurrentWeather.class.getResourceAsStream(resource)) {
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

    private static String coordinate(double value) {
        String magnitude =
                new BigDecimal(Math.abs(value)).setScale(6, RoundingMode.HALF_EVEN).toPlainString();
        return Double.doubleToRawLongBits(value) < 0 ? "-" + magnitude : magnitude;
    }

    private static QueryAttempt<GetCurrentWeatherOutput> rateLimit(Duration delay) {
        return retry(FailureKind.RATE_LIMIT, "QWeather rate limited the request", delay);
    }

    private static final Pattern OBSERVATION_TIME =
            Pattern.compile(
                    "([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2})(?::([0-9]{2})(?:[.,][0-9]+)?)?(?:Z|[+-]([0-9]{2}):([0-9]{2}))");

    private static boolean validTime(String text) {
        Matcher parts = OBSERVATION_TIME.matcher(text);
        if (!parts.matches()) return false;
        try {
            LocalDate.of(number(parts, 1), number(parts, 2), number(parts, 3));
        } catch (DateTimeException invalidDate) {
            return false;
        }
        return number(parts, 4) < 24
                && number(parts, 5) < 60
                && number(parts, 6) < 60
                && number(parts, 7) <= 24
                && number(parts, 8) <= 60;
    }

    private static int number(Matcher parts, int index) {
        return parts.group(index) == null ? 0 : Integer.parseInt(parts.group(index));
    }

    private static QueryAttempt<GetCurrentWeatherOutput> decodeWeather(
            byte[] body, Credentials credentials) {
        JsonNode data;
        try {
            data = lowercaseFields(JSON.readTree(body));
        } catch (IOException malformed) {
            return invalidData();
        }
        if (data == null || !data.isObject()) return invalidData();
        String text = data.path("condition").path("text").textValue();
        String code = data.path("condition").path("code").textValue();
        String unit = data.path("temperature").path("unit").textValue();
        JsonNode value = data.path("temperature").path("value");
        String time = data.path("validtime").textValue();
        JsonNode attributions = data.path("metadata").path("attributions");
        if (text == null
                || text.isEmpty()
                || credentials.containsCredential(text)
                || code == null
                || code.isEmpty()
                || credentials.containsCredential(code)
                || unit == null
                || unit.isEmpty()
                || credentials.containsCredential(unit)
                || !value.isNumber()
                || !Double.isFinite(value.doubleValue())
                || time == null
                || credentials.containsCredential(time)
                || !validTime(time)
                || !attributions.isArray()
                || attributions.size() < 1
                || attributions.size() > 16) return invalidData();
        List<String> credits = new ArrayList<>();
        for (JsonNode element : attributions) {
            String credit = element.textValue();
            if (credit == null
                    || credit.isEmpty()
                    || credit.getBytes(StandardCharsets.UTF_8).length > 500
                    || credentials.containsCredential(credit)) return invalidData();
            credits.add(credit);
        }
        return QueryAttempt.branch(
                "found",
                new GetCurrentWeatherOutput(text, code, value.doubleValue(), unit, time, credits),
                Receipt.empty());
    }

    private static JsonNode lowercaseFields(JsonNode value) {
        if (value == null || !value.isObject()) return value;
        com.fasterxml.jackson.databind.node.ObjectNode fields = JSON.createObjectNode();
        value.fields()
                .forEachRemaining(entry ->
                        fields.set(
                                entry.getKey().toLowerCase(java.util.Locale.ROOT),
                                lowercaseFields(entry.getValue())));
        return fields;
    }

    private static QueryAttempt<GetCurrentWeatherOutput> invalidData() {
        return failed(
                "invalidResponse", FailureKind.PROTOCOL, "QWeather returned invalid lookup data");
    }

    private static QueryAttempt<GetCurrentWeatherOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetCurrentWeatherOutput("", "", 0, "", "", Collections.emptyList()),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetCurrentWeatherOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
