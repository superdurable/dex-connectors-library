// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.haimou;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.io.IOException;
import java.io.InputStream;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.UnknownHostException;
import java.time.Duration;
import java.time.Instant;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.function.Function;

/** The HAIMOU location query and its operation-specific Dex Step configuration. */
public final class IPLocation {
    private static final String CONNECTOR = "haimou";
    private static final String OPERATION = "lookupIPLocation";
    private static final String ENDPOINT = "https://hmkj4399.com/v1/ip";
    private static final ObjectMapper JSON =
            new ObjectMapper().enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final QueryDefinition DEFINITION = loadDefinition();

    private IPLocation() {}

    /** A public IPv4 address to look up. */
    public static final class LookupIPLocationInput {
        /** Textual IPv4 address. */
        public final String ipAddress;

        /** Construct the lookup input. */
        @JsonCreator
        public LookupIPLocationInput(@JsonProperty("ipAddress") String ipAddress) {
            this.ipAddress = ipAddress;
        }
    }

    /** Coarse location and network fields returned by HAIMOU. */
    public static final class LookupIPLocationOutput {
        /** Country name. */
        public final String country;

        /** Province name. */
        public final String province;

        /** City name. */
        public final String city;

        /** District name. */
        public final String district;

        /** Operator name. */
        public final String operator;

        /** Restore the provider fields from a Dex payload. */
        @JsonCreator
        public LookupIPLocationOutput(
                @JsonProperty("country") String country,
                @JsonProperty("province") String province,
                @JsonProperty("city") String city,
                @JsonProperty("district") String district,
                @JsonProperty("operator") String operator) {
            this.country = country;
            this.province = province;
            this.city = city;
            this.district = district;
            this.operator = operator;
        }
    }

    /** Concrete Dex Step input for a location result. */
    public static final class LookupIPLocationResult extends QueryResult<LookupIPLocationOutput> {
        /** Restore the selected branch and its receipt from Dex. */
        @JsonCreator
        public LookupIPLocationResult(
                @JsonProperty("branch") String branch,
                @JsonProperty("value") LookupIPLocationOutput value,
                @JsonProperty("receipt") Receipt receipt,
                @JsonProperty("failure") Failure failure) {
            super(branch, value, receipt, failure);
        }
    }

    /** Bind the location query to a named connection and configure its branch targets. */
    public static <I>
            QueryStep.ConfigBuilder<
                            I,
                            LookupIPLocationInput,
                            LookupIPLocationOutput,
                            LookupIPLocationResult>
                    newLookupIPLocationStep(
                            String stepType,
                            Class<I> inputType,
                            StepAnnotations annotations,
                            Query<LookupIPLocationInput, LookupIPLocationOutput> operation,
                            ConnectionRef connection,
                            Function<I, LookupIPLocationInput> mapInput) {
        if (!CONNECTOR.equals(connection.getProvider())) {
            throw new IllegalArgumentException("HAIMOU connection provider is invalid");
        }
        return QueryStep.newConfig(
                stepType,
                inputType,
                annotations,
                operation,
                connection,
                mapInput,
                LookupIPLocationResult::new);
    }

    /** Performs one bounded, redirect-safe provider request per Execute attempt. */
    public static final class LookupIPLocationOperation
            implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
        private final CredentialProvider<SecretString> credentials;
        private final ProviderHttpTransport transport;
        private final int maxResponseBytes;

        /** Use the HTTP timeout and response size limit declared in the manifest. */
        public LookupIPLocationOperation(CredentialProvider<SecretString> credentials) {
            this(
                    credentials,
                    new ProviderHttpClient(
                            Duration.ofNanos(10000000000L), AddressPolicy.PUBLIC_HTTPS),
                    65536);
        }

        /**
         * Bind a call-time credential source, configured transport, and response limit (one byte to
         * sixteen MiB).
         */
        public LookupIPLocationOperation(
                CredentialProvider<SecretString> credentials,
                ProviderHttpTransport transport,
                int maxResponseBytes) {
            this.credentials = Objects.requireNonNull(credentials);
            this.transport = Objects.requireNonNull(transport);
            if (maxResponseBytes <= 0 || maxResponseBytes > 16 << 20) {
                throw new IllegalArgumentException(
                        "HAIMOU response limit must be between one byte and sixteen MiB");
            }
            this.maxResponseBytes = maxResponseBytes;
        }

        @Override
        public QueryDefinition definition() {
            return DEFINITION;
        }

        @Override
        public QueryAttempt<LookupIPLocationOutput> invoke(Call call, LookupIPLocationInput input)
                throws InterruptedException {
            String ip = publicIPv4(input.ipAddress);
            if (ip == null)
                return failed(
                        "defect",
                        FailureKind.VALIDATION,
                        "HAIMOU requires one public IPv4 address");
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
                        "HAIMOU credentials are unavailable or invalid");
            }
            Map<String, List<String>> parameters = new LinkedHashMap<>();
            parameters.put("ip", Collections.singletonList(ip));
            parameters.put("apikey", Collections.singletonList(key.reveal()));
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
                            "HAIMOU response exceeded the configured limit");
                } catch (IOException error) {
                    return retry(
                            FailureKind.AVAILABILITY,
                            "HAIMOU response was interrupted",
                            Duration.ZERO);
                }
                return decodeLocation(
                        body,
                        ProviderHttp.parseRetryAfter(
                                response.getHeaders().first("Retry-After"), Instant.now()));
            } catch (IOException error) {
                return retry(
                        FailureKind.AVAILABILITY, "HAIMOU lookup request failed", Duration.ZERO);
            }
        }
    }

    private static QueryDefinition loadDefinition() {
        String resource = "/META-INF/dex-connectors/haimou/operation-definitions.json";
        try (InputStream input = IPLocation.class.getResourceAsStream(resource)) {
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

    private static QueryAttempt<LookupIPLocationOutput> classifyStatus(
            ProviderHttpResponse response) {
        int status = response.getStatusCode();
        Duration delay =
                ProviderHttp.parseRetryAfter(
                        response.getHeaders().first("Retry-After"), Instant.now());
        if (status == 429)
            return retry(FailureKind.RATE_LIMIT, "HAIMOU rate limited the lookup", delay);
        if (status == 408 || status >= 500)
            return retry(
                    FailureKind.AVAILABILITY,
                    "HAIMOU lookup service is temporarily unavailable",
                    delay);
        FailureKind kind = FailureKind.PROVIDER_REJECTION;
        if (status == 401) kind = FailureKind.AUTHENTICATION;
        if (status == 403) kind = FailureKind.AUTHORIZATION;
        return failed("providerRejected", kind, "HAIMOU rejected lookup with HTTP " + status);
    }

    private static String publicIPv4(String value) {
        if (value == null
                || value.isEmpty()
                || value.length() > 45
                || !value.matches("[a-fA-F0-9:.]+")) return null;
        // Java also accepts abbreviated IPv4 and zero-padded octets. Go accepts neither spelling.
        if (value.indexOf('.') >= 0) {
            String suffix = value.substring(value.lastIndexOf(':') + 1);
            String[] octets = suffix.split("\\.", -1);
            if (octets.length != 4) return null;
            for (String octet : octets) {
                if (!octet.matches("0|[1-9][0-9]{0,2}") || Integer.parseInt(octet) > 255)
                    return null;
            }
        } else if (value.indexOf(':') < 0) return null;
        try {
            InetAddress address = InetAddress.getByName(value);
            if (!(address instanceof Inet4Address)
                    || address.isSiteLocalAddress()
                    || address.isLoopbackAddress()
                    || address.isLinkLocalAddress()
                    || address.isAnyLocalAddress()
                    || address.isMulticastAddress()) return null;
            return address.getHostAddress();
        } catch (UnknownHostException error) {
            return null;
        }
    }

    private static QueryAttempt<LookupIPLocationOutput> decodeLocation(
            byte[] body, Duration delay) {
        JsonNode response;
        try {
            response = lowercaseFields(JSON.readTree(body));
        } catch (IOException error) {
            response = null;
        }
        if (response == null
                || !response.path("code").isIntegralNumber()
                || !response.get("code").canConvertToLong()) return invalidData();
        JsonNode data = lowercaseFields(response.path("data"));
        if (!data.isMissingNode() && !data.isNull()) {
            if (!data.isObject()) return invalidData();
            for (String field : new String[] {"country", "province", "city", "district", "isp"}) {
                JsonNode value = data.get(field);
                if (value != null && !value.isNull() && !value.isTextual()) return invalidData();
            }
        }
        long code = response.get("code").longValue();
        if (code == 429 || code == 10006)
            return retry(FailureKind.RATE_LIMIT, "HAIMOU rate limited the lookup", delay);
        if (code >= 500 && code <= 599)
            return retry(
                    FailureKind.AVAILABILITY,
                    "HAIMOU lookup service is temporarily unavailable",
                    delay);
        if (code != 200) {
            FailureKind kind = FailureKind.PROVIDER_REJECTION;
            if (code == 10001 || code == 10002) kind = FailureKind.AUTHENTICATION;
            if (code == 10003) kind = FailureKind.AUTHORIZATION;
            if (code == 10005) kind = FailureKind.QUOTA_EXHAUSTED;
            return failed("providerRejected", kind, "HAIMOU rejected lookup with code " + code);
        }
        LookupIPLocationOutput output =
                new LookupIPLocationOutput(
                        data.path("country").asText(""),
                        data.path("province").asText(""),
                        data.path("city").asText(""),
                        data.path("district").asText(""),
                        data.path("isp").asText(""));
        if (output.country.matches("(?U)\\s*") || output.city.matches("(?U)\\s*"))
            return failed(
                    "invalidResponse",
                    FailureKind.PROTOCOL,
                    "HAIMOU returned incomplete location data");
        return QueryAttempt.branch("found", output, Receipt.empty());
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

    private static QueryAttempt<LookupIPLocationOutput> invalidData() {
        return failed(
                "invalidResponse", FailureKind.PROTOCOL, "HAIMOU returned invalid lookup data");
    }

    private static QueryAttempt<LookupIPLocationOutput> failed(
            String branch, FailureKind kind, String message) {
        return QueryAttempt.branch(
                branch,
                new LookupIPLocationOutput("", "", "", "", ""),
                Receipt.empty(),
                new Failure(kind, CONNECTOR, OPERATION, message));
    }

    private static QueryAttempt<LookupIPLocationOutput> retry(
            FailureKind kind, String message, Duration delay) {
        return QueryAttempt.retry(new Failure(kind, CONNECTOR, OPERATION, message), delay);
    }
}
