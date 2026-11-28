// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qweather;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/qweather/scenarios.json",
                "https://abc123.xy.qweatherapi.com",
                "qweather",
                "getCurrentWeather",
                "private-test-key",
                "getCurrentWeather-missingCredential",
                "getCurrentWeather-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        String name = scenario.get("name").asText();
        if (name.endsWith("-smallLimit")) limit = 16;
        String host =
                name.equals("getCurrentWeather-invalidHost")
                        ? "spoof.qweatherapi.com.example"
                        : "abc123.xy.qweatherapi.com";
        SecretString accountHost = new SecretString(host);
        CurrentWeather.GetCurrentWeatherOperation operation =
                new CurrentWeather.GetCurrentWeatherOperation(
                        c -> new CurrentWeather.Credentials(accountHost, credentials.resolve(c)),
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                CurrentWeather.GetCurrentWeatherInput.class));
    }
}
