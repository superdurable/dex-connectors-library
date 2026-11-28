// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.juhe;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/juhe/scenarios.json",
                "https://apis.juhe.cn",
                "juhe",
                "getCurrentWeather",
                "test-juhe-api-key",
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
                new CurrentWeather.GetCurrentWeatherOperation(
                        credentials, transport, "https://apis.juhe.cn", limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                CurrentWeather.GetCurrentWeatherInput.class));
    }
}
