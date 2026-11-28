// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qingflow;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/qingflow/scenarios.json",
                "https://api.qingflow.com",
                "qingflow",
                "listApplications",
                "private-test-key",
                "listApplications-defect",
                "listApplications-oversized");
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
                new ApplicationDirectory.ListApplicationsOperation(credentials, transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                ApplicationDirectory.ListApplicationsInput.class));
    }
}
