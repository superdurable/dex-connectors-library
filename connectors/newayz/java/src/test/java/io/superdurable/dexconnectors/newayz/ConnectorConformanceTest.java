// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.newayz;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/newayz/scenarios.json",
                "https://api.newayz.com",
                "newayz",
                "getCurrentWeather",
                "test-newayz-access-key",
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
        if (scenario.get("name").asText().endsWith("-smallLimit")) limit = 16;
        CurrentWeather.GetCurrentWeatherOperation operation =
                new CurrentWeather.GetCurrentWeatherOperation(credentials, transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                CurrentWeather.GetCurrentWeatherInput.class));
    }
}
