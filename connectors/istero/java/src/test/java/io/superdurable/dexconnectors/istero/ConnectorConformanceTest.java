// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.istero;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/istero/scenarios.json",
                "https://api.istero.com",
                "istero",
                "getAccountStatus",
                "private-test-token",
                "getAccountStatus-defect",
                "getAccountStatus-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        AccountStatus.GetAccountStatusOperation operation =
                new AccountStatus.GetAccountStatusOperation(
                        callContext ->
                                new AccountStatus.Credentials(
                                        scenario.get("name").asText().endsWith("-invalidISID")
                                                ? "060066023"
                                                : "60066023",
                                        credentials.resolve(callContext)),
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"), AccountStatus.GetAccountStatusInput.class));
    }
}
