// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.caiyun;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.math.BigDecimal;
import java.math.MathContext;
import java.math.RoundingMode;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.ProviderException;
import java.security.SecureRandom;
import java.time.Clock;
import java.time.Duration;
import java.util.Base64;
import java.util.Objects;
import java.util.function.Function;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/** The Caiyun weather query and its operation-specific Dex Step configuration. */
public final class CurrentWeather {
    private static final String CONNECTOR = "caiyun";
    private static final String OPERATION = "getCurrentWeather";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private CurrentWeather() {}

    /** Signing credentials resolved from the connection for every attempt. */
    public static final class Credentials {
        /** Path-safe application key assigned by Caiyun. */
        public final SecretString appKey;

        /** Secret used to sign the request. */
        public final SecretString appSecret;

        /** Bind a provider application key to its signing secret. */
        public Credentials(SecretString appKey, SecretString appSecret) {
            this.appKey = Objects.requireNonNull(appKey);
            this.appSecret = Objects.requireNonNull(appSecret);
        }

        private boolean isValid() {
            return appKey.reveal().matches("[A-Za-z0-9_-]{1,256}")
                    && appSecret.reveal().matches("[!-~]{1,512}");
        }
    }

    /** A fresh 16-byte nonce for each Execute attempt. */
    @FunctionalInterface
    public interface NonceSource {
        /** Fill the supplied 16-byte buffer or throw when secure randomness is unavailable. */
        void fill(byte[] nonce) throws IOException;
    }

    /** GCJ-02 coordinates for mainland China and WGS84 coordinates elsewhere. */
    public static final class GetCurrentWeatherInput {
        /** Longitude in degrees east, from -180 to 180. */
        public final double longitude;

        /** Latitude in degrees north, from -90 to 90. */
        public final double latitude;

        /** Restore the location from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherInput(
                @JsonProperty("longitude") double longitude,
                @JsonProperty("latitude") double latitude) {
            this.longitude = longitude;
            this.latitude = latitude;
        }
    }

    /** Bounded metric values from Caiyun's realtime observation. */
    public static final class GetCurrentWeatherOutput {
        /** Air temperature at two meters, in Celsius. */
        public final double temperatureCelsius;

        /** Relative humidity as a fraction from zero to one. */
        public final double relativeHumidity;

        /** Provider weather condition code. */
        public final String skyCondition;

        /** Wind speed at ten meters, in meters per second. */
        public final double windSpeedMetersPerSecond;

        /** Restore the observation from a Dex payload. */
        @JsonCreator
        public GetCurrentWeatherOutput(
                @JsonProperty("temperatureCelsius") double temperatureCelsius,
                @JsonProperty("relativeHumidity") double relativeHumidity,
                @JsonProperty("skyCondition") String skyCondition,
                @JsonProperty("windSpeedMetersPerSecond") double windSpeedMetersPerSecond) {
            this.temperatureCelsius = temperatureCelsius;
            this.relativeHumidity = relativeHumidity;
            this.skyCondition = skyCondition;
            this.windSpeedMetersPerSecond = windSpeedMetersPerSecond;
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
            throw new IllegalArgumentException("Caiyun connection provider is invalid");
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
        private static final String DEFAULT_BASE_URL = "https://api.caiyunapp.com";
        private final CredentialProvider<Credentials> credentials;
        private final String baseURL;
        private final ProviderHttpTransport transport;
        private final long maxResponseBytes;
        private final Clock clock;
        private final NonceSource nonceSource;

        /**
         * Bind credentials with the manifest timeout of ten seconds and a 256 KiB response limit.
         */
        public GetCurrentWeatherOperation(CredentialProvider<Credentials> credentials) {
            this(credentials, DEFAULT_BASE_URL, Duration.ofSeconds(10), 262144);
        }

        /**
         * Bind the approved API origin or proxy, request timeout and positive response byte limit.
         */
        public GetCurrentWeatherOperation(
                CredentialProvider<Credentials> credentials,
                String baseURL,
                Duration timeout,
                long maxResponseBytes) {
            this(
                    credentials,
                    baseURL,
                    new ProviderHttpClient(
                            timeout,
                            ProviderHttp.validateBaseUrl(baseURL).startsWith("https://")
                                    ? AddressPolicy.PUBLIC_HTTPS
                                    : AddressPolicy.PRIVATE_NETWORK_HTTP),
                    maxResponseBytes,
                    Clock.systemUTC(),
                    new SecureRandom()::nextBytes);
        }

        /** Bind an injected transport with the default provider origin and secure random nonces. */
        public GetCurrentWeatherOperation(
                CredentialProvider<Credentials> credentials,
                ProviderHttpTransport transport,
                long maxResponseBytes) {
            this(
                    credentials,
                    DEFAULT_BASE_URL,
                    transport,
                    maxResponseBytes,
                    Clock.systemUTC(),
                    new SecureRandom()::nextBytes);
        }

        /** Bind a trusted transport, positive byte limit, clock and thread-safe nonce source. */
        public GetCurrentWeatherOperation(
                CredentialProvider<Credentials> credentials,
                String baseURL,
                ProviderHttpTransport transport,
                long maxResponseBytes,
                Clock clock,
                NonceSource nonceSource) {
            this.credentials = Objects.requireNonNull(credentials);
            this.baseURL = ProviderHttp.validateBaseUrl(baseURL);
            this.transport = Objects.requireNonNull(transport);
            this.clock = Objects.requireNonNull(clock);
            this.nonceSource = Objects.requireNonNull(nonceSource);
            if (maxResponseBytes <= 0)
                throw new IllegalArgumentException("Caiyun response byte limit must be positive");
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
                    || !(Math.abs(input.longitude) <= 180 && Math.abs(input.latitude) <= 90))
                return failed("defect", FailureKind.VALIDATION, "Caiyun coordinates are invalid");
            Credentials account;
            try {
                account = credentials.resolve(call);
            } catch (IOException unavailable) {
                account = null;
            }
            if (account == null || !account.isValid())
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "Caiyun signing credentials are unavailable or invalid");
            byte[] nonce = new byte[16];
            try {
                nonceSource.fill(nonce);
            } catch (IOException | ProviderException unavailable) {
                return retry(
                        FailureKind.AVAILABILITY,
                        "Caiyun request nonce could not be generated",
                        Duration.ZERO);
            }
            String key = account.appKey.reveal();
            String path =
                    "/v2.6/"
                            + key
                            + "/"
                            + coordinate(input.longitude)
                            + ","
                            + coordinate(input.latitude)
                            + "/realtime";
            String timestamp = Long.toString(clock.instant().getEpochSecond());
            String nonceText = hex(nonce);
            String canonical = String.join(":", "GET", path, "", key, nonceText, timestamp);
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", baseURL + path)
                            .header("Accept", "application/json")
                            .header("X-Cy-Nonce", nonceText)
                            .header("X-Cy-Timestamp", timestamp)
                            .header("X-Cy-Signature", signature(account.appSecret, canonical))
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                int status = response.getStatusCode();
                Duration delay =
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), clock.instant());
                if (status == 408 || status >= 500)
                    return retry(
                            FailureKind.AVAILABILITY, "Caiyun is temporarily unavailable", delay);
                if (status == 429)
                    return retry(
                            FailureKind.RATE_LIMIT, "Caiyun rate or quota limit reached", delay);
                if (status != 200) return rejected(status);
                try {
                    return decodeWeather(response.readBoundedBody(maxResponseBytes), account);
                } catch (BodyTooLargeException oversized) {
                    return failed(
                            "invalidResponse",
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Caiyun response exceeded the configured limit");
                }
            } catch (IOException unavailable) {
                return retry(
                        FailureKind.AVAILABILITY, "Caiyun weather request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/caiyun/operation-definitions.json";
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
        boolean negative = Double.doubleToRawLongBits(value) < 0;
        double magnitude = Math.abs(value);
        if (magnitude == 0) return negative ? "-0" : "0";
        BigDecimal exact = new BigDecimal(magnitude);
        // Go signs the shortest round-tripping decimal, including subnormal coordinates.
        for (int precision = 1; precision <= 17; precision++) {
            BigDecimal nearest = exact.round(new MathContext(precision, RoundingMode.HALF_EVEN));
            if (nearest.doubleValue() != magnitude) {
                int place = exact.precision() - exact.scale() - precision;
                BigDecimal step = BigDecimal.ONE.scaleByPowerOfTen(place);
                nearest = nearest.compareTo(exact) < 0 ? nearest.add(step) : nearest.subtract(step);
            }
            if (nearest.doubleValue() == magnitude)
                return (negative ? "-" : "") + nearest.stripTrailingZeros().toPlainString();
        }
        throw new IllegalStateException("No decimal representation for valid coordinate");
    }

    private static String hex(byte[] bytes) {
        char[] digits = "0123456789abcdef".toCharArray();
        char[] encoded = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            encoded[2 * i] = digits[(bytes[i] & 255) >>> 4];
            encoded[2 * i + 1] = digits[bytes[i] & 15];
        }
        return new String(encoded);
    }

    private static String signature(SecretString secret, String canonical) {
        try {
            Mac hmac = Mac.getInstance("HmacSHA256");
            hmac.init(
                    new SecretKeySpec(
                            secret.reveal().getBytes(StandardCharsets.UTF_8), "HmacSHA256"));
            return Base64.getUrlEncoder()
                    .encodeToString(hmac.doFinal(canonical.getBytes(StandardCharsets.UTF_8)));
        } catch (GeneralSecurityException invalidProvider) {
            throw new IllegalStateException("HmacSHA256 is unavailable", invalidProvider);
        }
    }

    private static QueryAttempt<GetCurrentWeatherOutput> rejected(int status) {
        FailureKind kind;
        switch (status) {
            case 400:
            case 401:
                kind = FailureKind.AUTHENTICATION;
                break;
            case 403:
                kind = FailureKind.AUTHORIZATION;
                break;
            case 422:
                kind = FailureKind.VALIDATION;
                break;
            default:
                kind = FailureKind.PROVIDER_REJECTION;
        }
        return failed("providerRejected", kind, "Caiyun rejected the weather request");
    }

    private static QueryAttempt<GetCurrentWeatherOutput> decodeWeather(
            byte[] body, Credentials account) {
        JsonNode document;
        try {
            document = lowercaseFields(JSON.readTree(new String(body, StandardCharsets.UTF_8)));
        } catch (IOException malformed) {
            return invalidData();
        }
        if (document == null || !"ok".equals(document.path("status").textValue()))
            return invalidData();
        JsonNode observation = document.path("result").path("realtime");
        JsonNode temperature = observation.path("temperature"),
                humidity = observation.path("humidity"),
                speed = observation.path("wind").path("speed");
        String sky = observation.path("skycon").textValue();
        if (!"ok".equals(observation.path("status").textValue())
                || !inRange(temperature, -150, 150)
                || !inRange(humidity, 0, 1)
                || !inRange(speed, 0, 200)
                || sky == null
                || sky.isEmpty()
                || sky.contains(account.appKey.reveal())
                || sky.contains(account.appSecret.reveal())) return invalidData();
        return QueryAttempt.branch(
                "fetched",
                new GetCurrentWeatherOutput(
                        temperature.doubleValue(),
                        humidity.doubleValue(),
                        sky,
                        speed.doubleValue()),
                Receipt.empty());
    }

    private static boolean inRange(JsonNode value, double min, double max) {
        return value.isNumber() && value.doubleValue() >= min && value.doubleValue() <= max;
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
                "invalidResponse", FailureKind.PROTOCOL, "Caiyun returned invalid lookup data");
    }

    private static QueryAttempt<GetCurrentWeatherOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new GetCurrentWeatherOutput(0, 0, "", 0),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<GetCurrentWeatherOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
