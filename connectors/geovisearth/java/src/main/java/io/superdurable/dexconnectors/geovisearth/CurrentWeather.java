// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.geovisearth;

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
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.function.Function;
import java.util.regex.*;

/** The GEOVIS Earth weather query and its operation-specific Dex Step configuration. */
public final class CurrentWeather {
    private static final String CONNECTOR = "geovisearth";
    private static final String OPERATION = "getCurrentWeather";
    private static final String DEFAULT_ORIGIN = "https://api.open.geovisearth.com";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private CurrentWeather() {}

    /** Location to query. */
    public static final class GetCurrentWeatherInput {
        /** Official area code or longitude,latitude pair in decimal degrees. */
        public final String location;

        /** Restore the weather fields from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherInput(@JsonProperty("location") String location) {
            this.location = location;
        }
    }

    /** Basic current weather observation. */
    public static final class GetCurrentWeatherOutput {
        /** AreaCode is the area code, blank for coordinate lookups. */
        public final String areaCode;

        /** Weather is the Chinese weather condition description. */
        public final String weather;

        /** WeatherCode is the provider's weather phenomenon code. */
        public final String weatherCode;

        /** TemperatureCelsius is air temperature in degrees Celsius. */
        public final double temperatureCelsius;

        /** RelativeHumidityPercent is relative humidity from 0 to 100 percent. */
        public final long relativeHumidityPercent;

        /** WindDirection is the human-readable wind direction. */
        public final String windDirection;

        /** WindForce is the human-readable wind-force level. */
        public final String windForce;

        /** SurfacePressureHpa is surface pressure in hectopascals. */
        public final double surfacePressureHpa;

        /** ObservedAt is the provider's local observation timestamp formatted yyyyMMddHHmmss. */
        public final String observedAt;

        /** Restore a weather observation from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherOutput(
                @JsonProperty("areaCode") String areaCode,
                @JsonProperty("weather") String weather,
                @JsonProperty("weatherCode") String weatherCode,
                @JsonProperty("temperatureCelsius") double temperatureCelsius,
                @JsonProperty("relativeHumidityPercent") long relativeHumidityPercent,
                @JsonProperty("windDirection") String windDirection,
                @JsonProperty("windForce") String windForce,
                @JsonProperty("surfacePressureHpa") double surfacePressureHpa,
                @JsonProperty("observedAt") String observedAt) {
            this.areaCode = areaCode;
            this.weather = weather;
            this.weatherCode = weatherCode;
            this.temperatureCelsius = temperatureCelsius;
            this.relativeHumidityPercent = relativeHumidityPercent;
            this.windDirection = windDirection;
            this.windForce = windForce;
            this.surfacePressureHpa = surfacePressureHpa;
            this.observedAt = observedAt;
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
            throw new IllegalArgumentException("GEOVIS Earth connection provider is invalid");
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

        /**
         * Bind credentials with the ten-second timeout and 256 KiB response limit from the
         * manifest.
         */
        public GetCurrentWeatherOperation(CredentialProvider<SecretString> credentials) {
            this(credentials, DEFAULT_ORIGIN, Duration.ofSeconds(10), 262144);
        }

        /** Bind the documented origin, timeout and positive response byte limit. */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                String baseURL,
                Duration timeout,
                long maxResponseBytes) {
            this(
                    credentials,
                    new ProviderHttpClient(timeout, AddressPolicy.PUBLIC_HTTPS),
                    maxResponseBytes);
            if (!DEFAULT_ORIGIN.equals(ProviderHttp.validateBaseUrl(baseURL)))
                throw new IllegalArgumentException(
                        "GEOVIS Earth requires its documented HTTPS origin");
        }

        /** Bind a trusted transport and positive response byte limit. */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                long maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            this.endpoint = DEFAULT_ORIGIN + "/v2/cn/area/basic";
            if (maxResponseBytes <= 0)
                throw new IllegalArgumentException("GEOVIS Earth response limit must be positive");
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
            if (!validLocation(location))
                return failed("defect", FailureKind.VALIDATION, "GEOVIS Earth location is invalid");
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
                        "GEOVIS Earth credentials are unavailable or invalid");
            }
            Map<String, List<String>> parameters = new LinkedHashMap<>();
            parameters.put("location", Collections.singletonList(location));
            parameters.put("token", Collections.singletonList(key.reveal()));
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
                            FailureKind.RATE_LIMIT, "GEOVIS Earth rate limited the request", delay);
                if (status == 408 || status >= 500)
                    return retry(
                            FailureKind.AVAILABILITY,
                            "GEOVIS Earth is temporarily unavailable",
                            delay);
                byte[] body;
                try {
                    body = response.readBoundedBody(maxResponseBytes);
                } catch (BodyTooLargeException error) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "GEOVIS Earth response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "GEOVIS Earth response was interrupted",
                            Duration.ZERO);
                }
                return status == 200
                        ? decodeWeather(body, key.reveal())
                        : classifyRejection(status);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "GEOVIS Earth weather request failed",
                        Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/geovisearth/operation-definitions.json";
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

    private static QueryAttempt<GetCurrentWeatherOutput> classifyRejection(int status) {
        FailureKind kind =
                status == 401
                        ? FailureKind.AUTHENTICATION
                        : status == 403
                                ? FailureKind.AUTHORIZATION
                                : FailureKind.PROVIDER_REJECTION;
        return failed("providerRejected", kind, "GEOVIS Earth rejected the weather request");
    }

    private static QueryAttempt<GetCurrentWeatherOutput> decodeWeather(byte[] body, String token) {
        JsonNode data = readFields(body);
        if (data == null || !data.isObject()) return invalidData();
        JsonNode status = data.path("status"),
                result = data.path("result"),
                date = data.path("date"),
                location = data.path("location");
        if (!status.isIntegralNumber()
                || !status.canConvertToLong()
                || !optionalObject(result)
                || !optionalObject(date)
                || !optionalObject(location)) return invalidData();
        JsonNode humidity = result.path("rh"),
                temperature = result.path("tem"),
                pressure = result.path("prs");
        if (!optionalString(result.path("wp"))
                || !optionalString(result.path("wp_code"))
                || !optionalString(result.path("wd_desc"))
                || !optionalString(result.path("ws_desc"))
                || !optionalString(location.path("areacode"))
                || !optionalString(date.path("time"))
                || !(absent(humidity) || humidity.isIntegralNumber() && humidity.canConvertToLong())
                || !optionalNumber(temperature)
                || !optionalNumber(pressure)) return invalidData();
        if (status.longValue() != 0)
            return failed(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "GEOVIS Earth rejected the weather request");
        String weather = result.path("wp").asText(""),
                code = result.path("wp_code").asText(""),
                direction = result.path("wd_desc").asText(""),
                force = result.path("ws_desc").asText("");
        String area = location.path("areacode").asText(""), time = date.path("time").asText("");
        if (weather.isEmpty()
                || code.isEmpty()
                || direction.isEmpty()
                || force.isEmpty()
                || absent(humidity)
                || humidity.longValue() < 0
                || humidity.longValue() > 100
                || pressure.asDouble() <= 0
                || !validTime(time)) return invalidData();
        for (String field : Arrays.asList(area, weather, code, direction, force, time))
            if (field.contains(token)) return invalidData();
        return QueryAttempt.branch(
                "fetched",
                new GetCurrentWeatherOutput(
                        area,
                        weather,
                        code,
                        temperature.asDouble(),
                        humidity.longValue(),
                        direction,
                        force,
                        pressure.asDouble(),
                        time),
                Receipt.empty());
    }

    private static boolean absent(JsonNode n) {
        return n.isMissingNode() || n.isNull();
    }

    private static boolean optionalObject(JsonNode n) {
        return absent(n) || n.isObject();
    }

    private static boolean optionalString(JsonNode n) {
        return absent(n) || n.isTextual();
    }

    private static boolean optionalNumber(JsonNode n) {
        return absent(n) || n.isNumber() && Double.isFinite(n.doubleValue());
    }

    private static boolean validLocation(String location) {
        if (location == null) return false;
        if (location.matches("WTX_CH[0-9]{9}")) return true;
        String[] pair = location.split(",", -1);
        return pair.length == 2 && validCoordinate(pair[0], 180) && validCoordinate(pair[1], 90);
    }

    private static final Pattern COORDINATE =
            Pattern.compile(
                    "[+-]?(?:(?:[0-9](?:_?[0-9])*+(?:\\.(?:[0-9](?:_?[0-9])*+)?)?|\\.[0-9](?:_?[0-9])*+)(?:[Ee][+-]?[0-9](?:_?[0-9])*+)?|0[xX](?:_?[0-9A-Fa-f](?:_?[0-9A-Fa-f])*+(?:\\.(?:[0-9A-Fa-f](?:_?[0-9A-Fa-f])*+)?)?|\\.[0-9A-Fa-f](?:_?[0-9A-Fa-f])*+)[pP][+-]?[0-9](?:_?[0-9])*+)");

    private static boolean validCoordinate(String text, double maximum) {
        return COORDINATE.matcher(text).matches()
                && Math.abs(Double.parseDouble(text.replace("_", ""))) <= maximum;
    }

    private static boolean validTime(String text) {
        if (!text.matches("[0-9]{14}")) return false;
        try {
            java.time.LocalDateTime.of(
                    Integer.parseInt(text.substring(0, 4)),
                    Integer.parseInt(text.substring(4, 6)),
                    Integer.parseInt(text.substring(6, 8)),
                    Integer.parseInt(text.substring(8, 10)),
                    Integer.parseInt(text.substring(10, 12)),
                    Integer.parseInt(text.substring(12, 14)));
            return true;
        } catch (DateTimeException invalid) {
            return false;
        }
    }

    private static JsonNode readFields(byte[] body) {
        try {
            return lowercaseFields(JSON.readTree(new String(body, StandardCharsets.UTF_8)));
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
                "invalidResponse",
                FailureKind.PROTOCOL,
                "GEOVIS Earth returned invalid lookup data");
    }

    private static QueryAttempt<GetCurrentWeatherOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetCurrentWeatherOutput("", "", "", 0, 0, "", "", 0, ""),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetCurrentWeatherOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
