// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.apizero;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/apizero/scenarios.json",
                "https://v1.apizero.cn",
                "apizero",
                "getAccountInfo",
                "sk_live_test-secret",
                "getAccountInfo-defect",
                "getAccountInfo-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        AccountInfo.GetAccountInfoOperation operation =
                new AccountInfo.GetAccountInfoOperation(
                        scenario.get("name").asText().endsWith("-defect")
                                ? ignored -> new SecretString("invalid")
                                : credentials,
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), AccountInfo.GetAccountInfoInput.class));
    }
}
