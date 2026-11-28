// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.seniverse;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.time.DateTimeException;
import java.time.Duration;
import java.time.Instant;
import java.time.LocalDate;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.function.Function;
import java.util.regex.*;

/** The Seniverse weather query and its operation-specific Dex Step configuration. */
public final class CurrentWeather {
    private static final String CONNECTOR = "seniverse";
    private static final String OPERATION = "getCurrentWeather";
    private static final String DEFAULT_ORIGIN = "https://api.seniverse.com";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private CurrentWeather() {}

    /** Location to query. */
    public static final class GetCurrentWeatherInput {
        /**
         * Location is a Seniverse city ID, Chinese city name, pinyin name, or latitude:longitude
         * pair.
         */
        public final String location;

        /** Restore the weather fields from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherInput(@JsonProperty("location") String location) {
            this.location = location;
        }
    }

    /** Current weather reported by Seniverse. */
    public static final class GetCurrentWeatherOutput {
        /** LocationId is Seniverse's city ID. */
        public final String locationId;

        /** LocationName is Seniverse's localized city name. */
        public final String locationName;

        /** Country is the two-letter country code. */
        public final String country;

        /** WeatherText is the localized current weather condition. */
        public final String weatherText;

        /** WeatherCode is Seniverse's weather condition code. */
        public final String weatherCode;

        /** Temperature is the provider's numeric temperature string. */
        public final String temperature;

        /** TemperatureUnit is c for Celsius or f for Fahrenheit. */
        public final String temperatureUnit;

        /** LastUpdate is the provider's RFC3339 local observation time. */
        public final String lastUpdate;

        /** Restore the weather fields from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherOutput(
                @JsonProperty("locationId") String locationId,
                @JsonProperty("locationName") String locationName,
                @JsonProperty("country") String country,
                @JsonProperty("weatherText") String weatherText,
                @JsonProperty("weatherCode") String weatherCode,
                @JsonProperty("temperature") String temperature,
                @JsonProperty("temperatureUnit") String temperatureUnit,
                @JsonProperty("lastUpdate") String lastUpdate) {
            this.locationId = locationId;
            this.locationName = locationName;
            this.country = country;
            this.weatherText = weatherText;
            this.weatherCode = weatherCode;
            this.temperature = temperature;
            this.temperatureUnit = temperatureUnit;
            this.lastUpdate = lastUpdate;
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
            throw new IllegalArgumentException("Seniverse connection provider is invalid");
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
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final long maxResponseBytes;
        private final String endpoint;
        private final String language;
        private final String unit;

        /** Bind the call-time credential provider with manifest defaults. */
        public GetCurrentWeatherOperation(CredentialProvider<SecretString> credentials) {
            this(credentials, DEFAULT_ORIGIN, "zh-Hans", "c", Duration.ofSeconds(10), 262144);
        }

        /**
         * Use an approved base URL, response language, temperature unit, timeout and byte limit.
         */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                String baseURL,
                String language,
                String unit,
                Duration timeout,
                long maxResponseBytes) {
            this(
                    credentials,
                    baseURL,
                    language,
                    unit,
                    new ProviderHttpClient(
                            timeout,
                            ProviderHttp.validateBaseUrl(baseURL).startsWith("https://")
                                    ? AddressPolicy.PUBLIC_HTTPS
                                    : AddressPolicy.PRIVATE_NETWORK_HTTP),
                    maxResponseBytes);
        }

        /** Supply a bounded transport with default provider settings. */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                long maxResponseBytes) {
            this(credentials, DEFAULT_ORIGIN, "zh-Hans", "c", transport, maxResponseBytes);
        }

        /** Bind an injected transport and provider settings. Construction makes no request. */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                String baseURL,
                String language,
                String unit,
                ProviderHttpTransport transport,
                long maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            this.endpoint = ProviderHttp.validateBaseUrl(baseURL) + "/v3/weather/now.json";
            if (!Arrays.asList(
                                    "zh-Hans", "zh-Hant", "en", "ja", "de", "fr", "es", "pt", "hi",
                                    "id", "ru", "th", "ar")
                            .contains(language)
                    || !("c".equals(unit) || "f".equals(unit))
                    || maxResponseBytes <= 0)
                throw new IllegalArgumentException("Invalid Seniverse request settings");
            this.language = language;
            this.unit = unit;
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<GetCurrentWeatherOutput> invoke(Call call, GetCurrentWeatherInput input)
                throws InterruptedException {
            String location = input == null ? null : input.location;
            if (location == null
                    || location.isEmpty()
                    || location.getBytes(StandardCharsets.UTF_8).length > 256
                    || location.matches("(?sU)^\\s.*|.*\\s$"))
                return failed("defect", FailureKind.VALIDATION, "Seniverse location is invalid");
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
                        "Seniverse credentials are unavailable or invalid");
            }
            Map<String, List<String>> parameters = new LinkedHashMap<>();
            parameters.put("location", Collections.singletonList(location));
            parameters.put("key", Collections.singletonList(key.reveal()));
            parameters.put("language", Collections.singletonList(language));
            parameters.put("unit", Collections.singletonList(unit));
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET",
                                    endpoint + "?" + ProviderHttp.encodeQueryValues(parameters))
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                Duration delay =
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now());
                if (status == 429)
                    return retry(
                            FailureKind.RATE_LIMIT, "Seniverse rate limited the request", delay);
                if (status == 408 || status >= 500)
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Seniverse is temporarily unavailable",
                            delay);
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Seniverse response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Seniverse response was interrupted",
                            Duration.ZERO);
                }
                return status == 200
                        ? decodeWeather(body, unit, key.reveal())
                        : classifyRejection(body, status, delay);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "Seniverse weather request failed",
                        Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/seniverse/operation-definitions.json";
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

    private static QueryAttempt<GetCurrentWeatherOutput> classifyRejection(
            byte[] body, int status, Duration delay) {
        JsonNode problem = readFields(body);
        String code = problem == null ? "" : problem.path("status_code").asText("");
        FailureKind kind;
        switch (code) {
            case "AP010014":
                return retry(FailureKind.RATE_LIMIT, "Seniverse rate limited the request", delay);
            case "AP010003":
            case "AP010004":
                kind = FailureKind.AUTHENTICATION;
                break;
            case "AP010002":
            case "AP010006":
            case "AP010008":
            case "AP010009":
                kind = FailureKind.AUTHORIZATION;
                break;
            case "AP010012":
            case "AP010013":
                kind = FailureKind.QUOTA_EXHAUSTED;
                break;
            case "AP010001":
            case "AP010010":
            case "AP010011":
            case "AP010017":
                kind = FailureKind.VALIDATION;
                break;
            default:
                kind = FailureKind.PROVIDER_REJECTION;
        }
        return failed(
                "providerRejected", kind, "Seniverse rejected the request with HTTP " + status);
    }

    private static QueryAttempt<GetCurrentWeatherOutput> decodeWeather(
            byte[] body, String unit, String key) {
        JsonNode data = readFields(body);
        if (data == null) return invalidData();
        JsonNode results = data.path("results");
        if (!results.isArray() || results.size() != 1) return invalidData();
        JsonNode observation = lowercaseFields(results.get(0));
        JsonNode location = observation.path("location"), now = observation.path("now");
        String id = location.path("id").textValue(),
                name = location.path("name").textValue(),
                code = now.path("code").textValue(),
                text = now.path("text").textValue();
        JsonNode country = location.path("country");
        String temperature = now.path("temperature").textValue(),
                timestamp = observation.path("last_update").textValue();
        if (id == null
                || id.isEmpty()
                || name == null
                || name.isEmpty()
                || code == null
                || code.isEmpty()
                || text == null
                || text.isEmpty()
                || !(country.isMissingNode() || country.isNull() || country.isTextual())
                || temperature == null
                || !validTemperature(temperature)
                || timestamp == null
                || !validTime(timestamp)) return invalidData();
        for (String field :
                Arrays.asList(id, name, country.asText(""), text, code, temperature, timestamp)) {
            if (field.contains(key)) return invalidData();
        }
        return QueryAttempt.branch(
                "fetched",
                new GetCurrentWeatherOutput(
                        id, name, country.asText(""), text, code, temperature, unit, timestamp),
                Receipt.empty());
    }

    private static final Pattern TEMPERATURE =
            Pattern.compile(
                    "[+-]?(?:(?:[0-9](?:_?[0-9])*+(?:\\.(?:[0-9](?:_?[0-9])*+)?)?|\\.[0-9](?:_?[0-9])*+)(?:[Ee][+-]?[0-9](?:_?[0-9])*+)?|0[xX](?:_?[0-9A-Fa-f](?:_?[0-9A-Fa-f])*+(?:\\.(?:[0-9A-Fa-f](?:_?[0-9A-Fa-f])*+)?)?|\\.[0-9A-Fa-f](?:_?[0-9A-Fa-f])*+)[pP][+-]?[0-9](?:_?[0-9])*+)");

    private static boolean validTemperature(String text) {
        return TEMPERATURE.matcher(text).matches()
                && Double.isFinite(Double.parseDouble(text.replace("_", "")));
    }

    private static final Pattern OBSERVATION_TIME =
            Pattern.compile(
                    "([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,][0-9]+)?(?:Z|[+-]([0-9]{2}):([0-9]{2}))");

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

    private static JsonNode readFields(byte[] body) {
        try {
            return lowercaseFields(JSON.readTree(body));
        } catch (IOException invalid) {
            return null;
        }
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
                "invalidResponse", FailureKind.PROTOCOL, "Seniverse returned invalid lookup data");
    }

    private static QueryAttempt<GetCurrentWeatherOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetCurrentWeatherOutput("", "", "", "", "", "", "", ""),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetCurrentWeatherOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
