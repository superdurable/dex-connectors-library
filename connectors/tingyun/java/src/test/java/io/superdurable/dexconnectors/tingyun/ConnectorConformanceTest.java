// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tingyun;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/tingyun/scenarios.json",
                "https://wukong2.tingyun.com",
                "tingyun",
                "listApplications",
                "access-secret",
                "listApplications-defect",
                "listApplications-oversized");
    }

    @org.junit.jupiter.api.Test
    void rejectsUntrustedOrigin() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new ApplicationDirectory.ListApplicationsOperation(
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
        ApplicationDirectory.ListApplicationsOperation operation =
                new ApplicationDirectory.ListApplicationsOperation(
                        credentials, "https://wukong2.tingyun.com", transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                ApplicationDirectory.ListApplicationsInput.class));
    }
}
