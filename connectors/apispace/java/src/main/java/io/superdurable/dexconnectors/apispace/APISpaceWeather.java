// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.apispace;

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
import java.util.Objects;
import java.util.function.Function;

/** The APISpace weather query and its operation-specific Dex Step configuration. */
public final class APISpaceWeather {
    private static final String CONNECTOR = "apispace";
    private static final String OPERATION = "getCurrentWeather";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private APISpaceWeather() {}

    /** City identifier from the APISpace area list. */
    public static final class GetCurrentWeatherInput {
        /** One to 32 ASCII letters or digits. */
        public final String areaCode;

        /** Select the city identified by its area code. */
        @JsonCreator
        public GetCurrentWeatherInput(@JsonProperty("areaCode") String areaCode) {
            this.areaCode = areaCode;
        }
    }

    /** Current conditions reported for one area. */
    public static final class CurrentWeather {
        /** City display name. */
        public final String city;

        /** Weather description. */
        public final String condition;

        /** Air temperature in degrees Celsius. */
        public final double temperatureCelsius;

        /** Relative humidity between zero and one hundred percent. */
        public final int humidityPercent;

        /** Update time in Beijing local time. */
        public final String updatedAtBeijing;

        /** Restore weather fields from a Dex payload. */
        @JsonCreator
        public CurrentWeather(
                @JsonProperty("city") String city,
                @JsonProperty("condition") String condition,
                @JsonProperty("temperatureCelsius") double temperatureCelsius,
                @JsonProperty("humidityPercent") int humidityPercent,
                @JsonProperty("updatedAtBeijing") String updatedAtBeijing) {
            this.city = city;
            this.condition = condition;
            this.temperatureCelsius = temperatureCelsius;
            this.humidityPercent = humidityPercent;
            this.updatedAtBeijing = updatedAtBeijing;
        }
    }

    /** Concrete Dex Step input for a weather result. */
    public static final class GetCurrentWeatherResult extends QueryResult<CurrentWeather> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public GetCurrentWeatherResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") CurrentWeather value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the weather query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I, GetCurrentWeatherInput, CurrentWeather, GetCurrentWeatherResult>
                    newGetCurrentWeatherStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<GetCurrentWeatherInput, CurrentWeather> operation,
                            ConnectionRef connection,
                            Function<I, GetCurrentWeatherInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("APISpace connection provider is invalid");
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
            implements Query<GetCurrentWeatherInput, CurrentWeather> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final long maxResponseBytes;
        private final String endpoint;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public GetCurrentWeatherOperation(CredentialProvider<SecretString> credentials) {
            this(credentials, "https://eolink.o.apispace.com", Duration.ofSeconds(15), 1048576);
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
            this(credentials, transport, "https://eolink.o.apispace.com", maxResponseBytes);
        }

        /** Bind an injected transport to an approved base URL and positive byte limit. */
        public GetCurrentWeatherOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                String baseURL,
                long maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            this.endpoint = ProviderHttp.validateBaseUrl(baseURL) + "/456456/weather/v001/now";
            if (maxResponseBytes <= 0)
                throw new IllegalArgumentException("APISpace response limit must be positive");
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<CurrentWeather> invoke(Call call, GetCurrentWeatherInput input)
                throws InterruptedException {
            if (input.areaCode == null || !input.areaCode.matches("[A-Za-z0-9]{1,32}"))
                return failed("defect", FailureKind.VALIDATION, "APISpace area code is invalid");
            SecretString key;
            try {
                key = credentials.resolve(call);
            } catch (IOException error) {
                key = null;
            }
            if (key == null || !key.reveal().matches("[!-~]{1,2048}")) {
                return failed(
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "APISpace credentials are unavailable or invalid");
            }
            ProviderHttpRequest request =
                    ProviderHttpRequest.newBuilder("GET", endpoint + "?areacode=" + input.areaCode)
                            .header("Accept", "application/json")
                            .header("Content-Type", "application/json")
                            .header("Authorization-Type", "apikey")
                            .header("X-Apispace-Token", key.reveal())
                            .build();
            try (ProviderHttpResponse response = transport.send(request)) {
                if (response.getStatusCode() == 408
                        || response.getStatusCode() == 429
                        || response.getStatusCode() == 413
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
                            "APISpace response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "APISpace response was interrupted",
                            Duration.ZERO);
                }
                if (response.getStatusCode() != 200)
                    return failed(
                            "providerRejected",
                            httpRejectionKind(response.getStatusCode()),
                            "APISpace rejected the HTTP request");
                return decodeWeather(body);
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "APISpace weather request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/apispace/operation-definitions.json";
        try (InputStream input = APISpaceWeather.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<CurrentWeather> classifyStatus(ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429 || status == 413)
            return retry(FailureKind.RATE_LIMIT, "APISpace rate limited the lookup", delay);
        return retry(
                FailureKind.AVAILABILITY,
                "APISpace weather service is temporarily unavailable",
                delay);
    }

    private static FailureKind httpRejectionKind(int status) {
        switch (status) {
            case 401:
                return FailureKind.AUTHENTICATION;
            case 403:
            case 416:
                return FailureKind.AUTHORIZATION;
            case 400:
            case 404:
                return FailureKind.VALIDATION;
            default:
                return FailureKind.PROVIDER_REJECTION;
        }
    }

    private static QueryAttempt<CurrentWeather> decodeWeather(byte[] body) {
        JsonNode wire;
        try {
            wire = lowercaseFields(JSON.readTree(body));
        } catch (IOException invalid) {
            return invalidData();
        }
        if (wire == null
                || !wire.path("status").isIntegralNumber()
                || !wire.get("status").canConvertToLong()) return invalidData();
        JsonNode result = lowercaseFields(wire.path("result"));
        JsonNode location = lowercaseFields(result.path("location"));
        JsonNode live = lowercaseFields(result.path("realtime"));
        if (!optionalObject(result) || !optionalObject(location) || !optionalObject(live))
            return invalidData();
        JsonNode city = location.path("name"),
                condition = live.path("text"),
                updated = result.path("last_update"),
                temperature = live.path("temp"),
                humidity = live.path("rh");
        if (!optionalString(city)
                || !optionalString(condition)
                || !optionalString(updated)
                || (present(temperature)
                        && (!temperature.isNumber() || !Double.isFinite(temperature.doubleValue())))
                || (present(humidity)
                        && (!humidity.isIntegralNumber() || !humidity.canConvertToLong())))
            return invalidData();
        long status = wire.get("status").longValue();
        if (status != 0)
            return failed(
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    "APISpace rejected weather query with status " + status);
        if (city.asText("").isEmpty()
                || condition.asText("").isEmpty()
                || updated.asText("").isEmpty()
                || !present(temperature)
                || !present(humidity)
                || humidity.longValue() < 0
                || humidity.longValue() > 100) return invalidData();
        return QueryAttempt.branch(
                "found",
                new CurrentWeather(
                        city.textValue(),
                        condition.textValue(),
                        temperature.doubleValue(),
                        humidity.intValue(),
                        updated.textValue()),
                Receipt.empty());
    }

    private static boolean present(JsonNode value) {
        return !value.isMissingNode() && !value.isNull();
    }

    private static boolean optionalString(JsonNode value) {
        return !present(value) || value.isTextual();
    }

    private static boolean optionalObject(JsonNode value) {
        return !present(value) || value.isObject();
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

    private static QueryAttempt<CurrentWeather> invalidData() {
        return failed(
                "invalidResponse", FailureKind.PROTOCOL, "APISpace returned invalid lookup data");
    }

    private static QueryAttempt<CurrentWeather> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new CurrentWeather("", "", 0, 0, ""),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<CurrentWeather> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
