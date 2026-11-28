// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.caiyun;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/caiyun/scenarios.json",
                "https://api.caiyunapp.com",
                "caiyun",
                "getCurrentWeather",
                "test-app-secret",
                "getCurrentWeather-missing-credentials",
                "getCurrentWeather-oversized");
    }

    @org.junit.jupiter.api.Test
    void unavailableNonceRetriesBeforeDispatch() throws Exception {
        CurrentWeather.GetCurrentWeatherOperation operation =
                new CurrentWeather.GetCurrentWeatherOperation(
                        call ->
                                new CurrentWeather.Credentials(
                                        new SecretString("test-app-key"),
                                        new SecretString("test-app-secret")),
                        "https://api.caiyunapp.com",
                        request -> {
                            throw new AssertionError("an unsigned request reached the transport");
                        },
                        2048,
                        java.time.Clock.systemUTC(),
                        nonce -> {
                            throw new java.io.IOException("entropy unavailable");
                        });
        Call call =
                Call.create(
                        CallId.of("58acfd20-80ae-4aeb-8850-782757e16a28"),
                        new ConnectionRef("caiyun", "caiyun"),
                        new OperationRef("caiyun", "getCurrentWeather"),
                        null,
                        1);
        QueryAttempt<?> result =
                operation.invoke(call, new CurrentWeather.GetCurrentWeatherInput(116.3, 39.9));
        org.junit.jupiter.api.Assertions.assertTrue(result.isRetry());
        org.junit.jupiter.api.Assertions.assertEquals(
                FailureKind.AVAILABILITY, result.getFailure().getKind());
        org.junit.jupiter.api.Assertions.assertEquals(
                java.time.Duration.ZERO, result.getRetryAfter());
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        String name = scenario.path("name").asText();
        long limitBytes =
                name.equals("getCurrentWeather-oversized")
                        ? 8
                        : name.endsWith("-smallLimit") ? 16 : limit;
        byte[] nonce =
                new byte[] {
                    1,
                    (byte) 0x95,
                    (byte) 0xc6,
                    (byte) 0x8a,
                    0x42,
                    (byte) 0xe7,
                    0x72,
                    0x43,
                    (byte) 0xbf,
                    (byte) 0xf2,
                    (byte) 0xac,
                    (byte) 0x97,
                    (byte) 0xa7,
                    (byte) 0x8b,
                    (byte) 0x83,
                    0x7d
                };
        CurrentWeather.GetCurrentWeatherOperation operation =
                new CurrentWeather.GetCurrentWeatherOperation(
                        c ->
                                new CurrentWeather.Credentials(
                                        new SecretString("test-app-key"), credentials.resolve(c)),
                        "https://api.caiyunapp.com",
                        transport,
                        limitBytes,
                        java.time.Clock.fixed(
                                java.time.Instant.ofEpochSecond(1792654000),
                                java.time.ZoneOffset.UTC),
                        buffer -> System.arraycopy(nonce, 0, buffer, 0, buffer.length));
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                CurrentWeather.GetCurrentWeatherInput.class));
    }
}
