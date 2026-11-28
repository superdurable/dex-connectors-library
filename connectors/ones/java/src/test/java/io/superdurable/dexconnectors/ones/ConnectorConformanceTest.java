// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ones;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/ones/scenarios.json",
                "https://ones.example.org",
                "ones",
                "listTeams",
                "private-test-key",
                "listTeams-defect",
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
                        credentials, "https://ones.example.org", transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), TeamDirectory.ListTeamsInput.class));
    }
}
