// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.vika;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/vika/scenarios.json",
                "https://vika.cn",
                "vika",
                "listSpaces",
                "uskTestToken",
                "listSpaces-defect",
                "listSpaces-oversized");
    }

    @org.junit.jupiter.api.Test
    void rejectsUntrustedOrigin() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new SpaceDirectory.ListSpacesOperation(
                                call -> null,
                                "https://example.org/?q=invalid",
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
        SpaceDirectory.ListSpacesOperation operation =
                new SpaceDirectory.ListSpacesOperation(
                        credentials, "https://vika.cn", transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), SpaceDirectory.ListSpacesInput.class));
    }
}
