// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.juhe;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.time.Duration;
import java.time.Instant;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.function.Function;

/** The Juhe weather query and its operation-specific Dex Step configuration. */
public final class CurrentWeather {
    private static final String CONNECTOR = "juhe";
    private static final String OPERATION = "getCurrentWeather";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private CurrentWeather() {}

    /** City to query. */
    public static final class GetCurrentWeatherInput {
        /** City is the exact city name or ID from Juhe API 73's supported-city list. */
        public final String city;

        /** Restore the weather input from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherInput(@JsonProperty("city") String city) {
            this.city = city;
        }
    }

    /** Current conditions from Juhe. */
    public static final class GetCurrentWeatherOutput {
        /** City is the provider's returned city name. */
        public final String city;

        /** WeatherText is the current condition description. */
        public final String weatherText;

        /** WeatherCode is the condition ID from Juhe's weather-type list. */
        public final String weatherCode;

        /** TemperatureCelsius is the optional current temperature in Celsius. */
        public final String temperatureCelsius;

        /** HumidityPercent is the optional relative humidity percentage. */
        public final String humidityPercent;

        /** WindDirection is the optional wind direction text. */
        public final String windDirection;

        /** WindPower is the optional wind power text. */
        public final String windPower;

        /** AirQualityIndex is the optional AQI text. */
        public final String airQualityIndex;

        /** Restore the weather fields from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherOutput(
                @JsonProperty("city") String city,
                @JsonProperty("weatherText") String weatherText,
                @JsonProperty("weatherCode") String weatherCode,
                @JsonProperty("temperatureCelsius") String temperatureCelsius,
                @JsonProperty("humidityPercent") String humidityPercent,
                @JsonProperty("windDirection") String windDirection,
                @JsonProperty("windPower") String windPower,
                @JsonProperty("airQualityIndex") String airQualityIndex) {
            this.city = city;
            this.weatherText = weatherText;
            this.weatherCode = weatherCode;
            this.temperatureCelsius = temperatureCelsius;
            this.humidityPercent = humidityPercent;
            this.windDirection = windDirection;
            this.windPower = windPower;
            this.airQualityIndex = airQualityIndex;
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
            throw new IllegalArgumentException("Juhe connection provider is invalid");
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

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetCurrentWeatherOperation(CredentialProvider<SecretString> credentials) {
            this(credentials, "https://apis.juhe.cn", Duration.ofSeconds(10), 524288);
        }

        /**
         * Configure the approved provider base URL, positive per-attempt timeout, and response byte
         * limit.
         */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                String baseURL,
                Duration timeout,
                long maxResponseBytes) {
            this(
                    credentials,
                    new ProviderHttpClient(
                            timeout,
                            ProviderHttp.validateBaseUrl(baseURL).startsWith("https://")
                                    ? AddressPolicy.PUBLIC_HTTPS
                                    : AddressPolicy.PRIVATE_NETWORK_HTTP),
                    baseURL,
                    maxResponseBytes);
        }

        /** Inject the transport while retaining the default provider endpoint. */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                long maxResponseBytes) {
            this(credentials, transport, "https://apis.juhe.cn", maxResponseBytes);
        }

        /** Bind an injected transport to an approved base URL and positive byte limit. */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                String baseURL,
                long maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            this.endpoint = ProviderHttp.validateBaseUrl(baseURL) + "/simpleWeather/query";
            if (maxResponseBytes <= 0)
                throw new IllegalArgumentException("Juhe response limit must be positive");
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<GetCurrentWeatherOutput> invoke(Call call, GetCurrentWeatherInput input)
                throws InterruptedException {
            String city = input.city;
            if (!(city != null
                    && !city.isEmpty()
                    && !city.matches("(?sU)^\\s.*|.*\\s$")
                    && city.getBytes(java.nio.charset.StandardCharsets.UTF_8).length <= 128))
                return failed("defect", FailureKind.VALIDATION, "Juhe city is invalid");
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
                        "Juhe credentials are unavailable or invalid");
            }
            Map<String, List<String>> parameters = new LinkedHashMap<>();
            parameters.put("city", Collections.singletonList(city));
            parameters.put("key", Collections.singletonList(key.reveal()));
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("POST", endpoint)
                            .header("Accept", "application/json")
                            .header("Content-Type", "application/x-www-form-urlencoded")
                            .body(
                                    ProviderHttp.encodeQueryValues(parameters)
                                            .getBytes(java.nio.charset.StandardCharsets.UTF_8))
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                if (response.getStatusCode() == 408
                        || response.getStatusCode() == 429
                        || response.getStatusCode() >= 500) {
                    return classifyStatus(response);
                }
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Juhe response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Juhe response was interrupted",
                            Duration.ZERO);
                }
                if (response.getStatusCode() != 200)
                    return failed(
                            "providerRejected",
                            FailureKind.PROVIDER_REJECTION,
                            "Juhe rejected the HTTP request");
                return decodeWeather(body);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "Juhe weather request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/juhe/operation-definitions.json";
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

    private static QueryAttempt<GetCurrentWeatherOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "Juhe rate limited the lookup", delay);
        return retry(
                FailureKind.AVAILABILITY, "Juhe weather service is temporarily unavailable", delay);
    }

    private static QueryAttempt<GetCurrentWeatherOutput> decodeWeather(byte[] body) {
        JsonNode response;
        try {
            response = lowercaseFields(JSON.readTree(body));
        } catch (IOException error) {
            return invalidData();
        }
        if (response == null || !response.isObject()) return invalidData();
        JsonNode code = response.path("error_code");
        if (!code.isIntegralNumber() || !code.canConvertToLong()) return invalidData();
        JsonNode result = lowercaseFields(response.path("result"));
        if (!isOptionalObject(result) || !validStrings(result, "city")) return invalidData();
        JsonNode realtime = lowercaseFields(result.path("realtime"));
        if (!isOptionalObject(realtime)
                || !validStrings(
                        realtime,
                        "info",
                        "wid",
                        "temperature",
                        "humidity",
                        "direct",
                        "power",
                        "aqi")) return invalidData();
        if (code.longValue() != 0) return classifyWeatherError(code.longValue());
        String name = result.path("city").asText("");
        if (name.isEmpty()
                || realtime.path("info").asText("").isEmpty()
                || realtime.path("wid").asText("").isEmpty()) return invalidData();
        return QueryAttempt.branch(
                "fetched",
                new GetCurrentWeatherOutput(
                        name,
                        realtime.path("info").asText(""),
                        realtime.path("wid").asText(""),
                        realtime.path("temperature").asText(""),
                        realtime.path("humidity").asText(""),
                        realtime.path("direct").asText(""),
                        realtime.path("power").asText(""),
                        realtime.path("aqi").asText("")),
                Receipt.empty());
    }

    private static boolean isOptionalObject(JsonNode value) {
        return value.isObject() || value.isMissingNode() || value.isNull();
    }

    private static QueryAttempt<GetCurrentWeatherOutput> classifyWeatherError(long code) {
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (code >= Integer.MIN_VALUE && code <= Integer.MAX_VALUE) {
            switch ((int) code) {
                case 207303:
                case 10014:
                case 10020:
                    return retry(
                            FailureKind.AVAILABILITY,
                            "Juhe weather service is temporarily unavailable",
                            Duration.ZERO);
                case 10011:
                    return retry(
                            FailureKind.RATE_LIMIT, "Juhe limited the caller IP", Duration.ZERO);
                case 10001:
                case 10003:
                case 10009:
                    kind = FailureKind.AUTHENTICATION;
                    break;
                case 10002:
                case 10005:
                case 10007:
                case 10008:
                    kind = FailureKind.AUTHORIZATION;
                    break;
                case 10012:
                case 10013:
                    kind = FailureKind.QUOTA_EXHAUSTED;
                    break;
                case 207301:
                case 207302:
                    kind = FailureKind.VALIDATION;
                    break;
                default:
                    break;
            }
        }
        return failed("providerRejected", kind, "Juhe rejected the weather request");
    }

    private static boolean validStrings(JsonNode data, String... fields) {
        for (String field : fields) {
            JsonNode value = data.get(field);
            if (value != null && !value.isNull() && !value.isTextual()) return false;
        }
        return true;
    }

    private static JsonNode lowercaseFields(JsonNode value) {
        if (value == null || !value.isObject()) return value;
        com.fasterxml.jackson.databind.node.ObjectNode fields = JSON.createObjectNode();
        value.fields()
                .forEachRemaining(entry ->
                        fields.set(
                                entry.getKey().toLowerCase(java.util.Locale.ROOT),
                                entry.getValue()));
        return fields;
    }

    private static QueryAttempt<GetCurrentWeatherOutput> invalidData() {
        return failed("invalidResponse", FailureKind.PROTOCOL, "Juhe returned invalid lookup data");
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
