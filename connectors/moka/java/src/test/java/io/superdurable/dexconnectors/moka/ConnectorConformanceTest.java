// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.moka;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/moka/scenarios.json",
                "https://api.mokahr.com",
                "moka",
                "listJobFunctions",
                "secret-api-key",
                "listJobFunctions-defect",
                "listJobFunctions-oversized");
    }

    @org.junit.jupiter.api.Test
    void rejectsInvalidSettings() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new JobFunctionDirectory.ListJobFunctionsOperation(
                                call -> null, "org/path", "social"));
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new JobFunctionDirectory.ListJobFunctionsOperation(
                                call -> null, "org", "internal"));
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        JobFunctionDirectory.ListJobFunctionsOperation operation =
                new JobFunctionDirectory.ListJobFunctionsOperation(
                        credentials, "orgid-test", "social", transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                JobFunctionDirectory.ListJobFunctionsInput.class));
    }
}
