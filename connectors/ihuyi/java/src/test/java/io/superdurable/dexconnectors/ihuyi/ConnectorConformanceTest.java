// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ihuyi;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/ihuyi/scenarios.json",
                "https://api.ihuyi.com",
                "ihuyi",
                "lookupIPLocation",
                "private-test-key",
                null,
                "lookupIPLocation-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        IPLocation.LookupIPLocationOperation operation =
                new IPLocation.LookupIPLocationOperation(context -> new IPLocation.Credentials("test-account", credentials.resolve(context)), transport, limit, java.time.Clock.fixed(java.time.Instant.parse(scenario.get("fixedClock").asText()), java.time.ZoneOffset.UTC));
        return operation.invoke(
                call,
                new ObjectMapper().treeToValue(scenario.get("input"), IPLocation.LookupIPLocationInput.class));
    }
}
