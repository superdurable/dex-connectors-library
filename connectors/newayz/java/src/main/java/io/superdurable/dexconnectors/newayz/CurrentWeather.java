// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.newayz;

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

/** The WAYZ weather query and its operation-specific Dex Step configuration. */
public final class CurrentWeather {
    private static final String CONNECTOR = "newayz";
    private static final String OPERATION = "getCurrentWeather";
    private static final String ENDPOINT = "https://api.newayz.com/location/scenario/v1/weather";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private CurrentWeather() {}

    /** City to query. */
    public static final class GetCurrentWeatherInput {
        /** CityCode is a six-digit administrative code such as 420100 for Wuhan. */
        public final String cityCode;

        /** Restore the weather input from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherInput(@JsonProperty("cityCode") String cityCode) {
            this.cityCode = cityCode;
        }
    }

    /** Current conditions from WAYZ. */
    public static final class GetCurrentWeatherOutput {
        /** Province is the province name. */
        public final String province;

        /** City is the city name. */
        public final String city;

        /** CityCode is the administrative code returned by WAYZ. */
        public final String cityCode;

        /** Weather is the current weather description. */
        public final String weather;

        /** TemperatureCelsius is the observed Celsius temperature as a decimal string. */
        public final String temperatureCelsius;

        /** WindDirection is the current wind direction. */
        public final String windDirection;

        /** WindPower is the provider wind scale string. */
        public final String windPower;

        /** HumidityPercent is the relative humidity percent as a decimal string. */
        public final String humidityPercent;

        /** ReportTime is the provider observation timestamp string. */
        public final String reportTime;

        /** Restore the weather fields from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherOutput(
                @JsonProperty("province") String province,
                @JsonProperty("city") String city,
                @JsonProperty("cityCode") String cityCode,
                @JsonProperty("weather") String weather,
                @JsonProperty("temperatureCelsius") String temperatureCelsius,
                @JsonProperty("windDirection") String windDirection,
                @JsonProperty("windPower") String windPower,
                @JsonProperty("humidityPercent") String humidityPercent,
                @JsonProperty("reportTime") String reportTime) {
            this.province = province;
            this.city = city;
            this.cityCode = cityCode;
            this.weather = weather;
            this.temperatureCelsius = temperatureCelsius;
            this.windDirection = windDirection;
            this.windPower = windPower;
            this.humidityPercent = humidityPercent;
            this.reportTime = reportTime;
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
            throw new IllegalArgumentException("WAYZ connection provider is invalid");
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
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetCurrentWeatherOperation(CredentialProvider<SecretString> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(
                            Duration.ofNanos(10000000000L), AddressPolicy.PUBLIC_HTTPS),
                    262144);
        }

        /**
         * Bind a call-time credential source, configured transport, and response limit (one byte to
         * sixteen MiB).
         */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > 16 << 20) {
                throw new IllegalArgumentException(
                        "WAYZ response limit must be between one byte and sixteen MiB");
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
            String city = input.cityCode;
            if (!(city != null && city.matches("[0-9]{6}")))
                return failed("defect", FailureKind.VALIDATION, "WAYZ city is invalid");
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !key.reveal().matches("[!-~]{1,1024}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "WAYZ credentials are unavailable or invalid");
            }
            Map<String, List<String>> parameters = new LinkedHashMap<>();
            parameters.put("city", Collections.singletonList(city));
            parameters.put("access_key", Collections.singletonList(key.reveal()));
            parameters.put("extensions", Collections.singletonList("base"));
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder(
                                    "GET",
                                    ENDPOINT + "?" + ProviderHttp.encodeQueryValues(parameters))
                            .header("Accept", "application/json")
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                if (response.getStatusCode() != 200) {
                    return classifyStatus(response);
                }
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "WAYZ response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "WAYZ response was interrupted",
                            Duration.ZERO);
                }
                return decodeWeather(body, city);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "WAYZ weather request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/newayz/operation-definitions.json";
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
            return retry(FailureKind.RATE_LIMIT, "WAYZ rate limited the lookup", delay);
        if (status == 408 || status >= 500)
            return retry(
                    FailureKind.AVAILABILITY,
                    "WAYZ lookup service is temporarily unavailable",
                    delay);
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (status == 401) kind = FailureKind.AUTHENTICATION;
        if (status == 403) kind = FailureKind.AUTHORIZATION;
        return failed("providerRejected", kind, "WAYZ rejected lookup with HTTP " + status);
    }

    private static QueryAttempt<GetCurrentWeatherOutput> decodeWeather(byte[] body, String city) {
        JsonNode response;
        try {
            response = lowercaseFields(JSON.readTree(body));
        } catch (IOException error) {
            return invalidData();
        }
        if (response == null || !response.isObject()) return invalidData();
        if (!validStrings(response, "status", "infocode")
                || response.path("status").asText("").isEmpty()) return invalidData();
        JsonNode lives = response.path("lives");
        if (!lives.isArray() && !lives.isNull() && !lives.isMissingNode()) return invalidData();
        JsonNode live = null;
        for (JsonNode observation : lives) {
            live = lowercaseFields(observation);
            if ((!live.isObject() && !live.isNull())
                    || !validStrings(
                            live,
                            "province",
                            "city",
                            "adcode",
                            "weather",
                            "temperature",
                            "winddirection",
                            "windpower",
                            "humidity",
                            "reporttime")) return invalidData();
        }
        String code = response.path("infocode").asText("");
        if (!"1".equals(response.path("status").textValue()) || !"10000".equals(code))
            return failed(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "WAYZ rejected weather query with code "
                            + (code.matches("[0-9]{1,16}") ? code : "unknown"));
        if (lives.size() != 1
                || live == null
                || !city.equals(live.path("adcode").textValue())
                || live.path("city").asText("").isEmpty()
                || live.path("weather").asText("").isEmpty()
                || live.path("temperature").asText("").isEmpty()) return invalidData();
        return QueryAttempt.branch(
                "fetched",
                new GetCurrentWeatherOutput(
                        live.path("province").asText(""),
                        live.path("city").asText(""),
                        live.path("adcode").asText(""),
                        live.path("weather").asText(""),
                        live.path("temperature").asText(""),
                        live.path("winddirection").asText(""),
                        live.path("windpower").asText(""),
                        live.path("humidity").asText(""),
                        live.path("reporttime").asText("")),
                Receipt.empty());
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
        return failed("invalidResponse", FailureKind.PROTOCOL, "WAYZ returned invalid lookup data");
    }

    private static QueryAttempt<GetCurrentWeatherOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetCurrentWeatherOutput("", "", "", "", "", "", "", "", ""),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetCurrentWeatherOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
