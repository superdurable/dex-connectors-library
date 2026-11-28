// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rainbond;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/rainbond/scenarios.json",
                "https://rainbond.example.org",
                "rainbond",
                "listTeams",
                "private-test-key",
                "",
                "listTeams-oversized");
    }

    @org.junit.jupiter.api.Test
    void rejectsUntrustedOrigin() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new TeamDirectory.ListTeamsOperation(
                                call -> null,
                                "https://example.org/prefix",
                                request -> {
                                    throw new AssertionError();
                                },
                                2097152));
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        TeamDirectory.ListTeamsOperation operation =
                new TeamDirectory.ListTeamsOperation(
                        credentials, "https://rainbond.example.org", transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), TeamDirectory.ListTeamsInput.class));
    }
}
