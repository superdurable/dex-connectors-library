// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.opencsg;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/opencsg/scenarios.json",
                "https://ai.space.opencsg.com",
                "opencsg",
                "listGatewayModels",
                "test-secret",
                "listGatewayModels-defect",
                "listGatewayModels-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        String name = scenario.path("name").asText();
        if (name.equals("listGatewayModels-longKey"))
            credentials =
                    callContext ->
                            new SecretString(
                                    String.join("", java.util.Collections.nCopies(1024, "x")));
        if (name.equals("listGatewayModels-nonAsciiKey"))
            credentials = callContext -> new SecretString("密钥");
        if (name.endsWith("-smallLimit")) limit = 16;
        GatewayCatalog.ListGatewayModelsOperation operation =
                new GatewayCatalog.ListGatewayModelsOperation(credentials, transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                GatewayCatalog.ListGatewayModelsInput.class));
    }
}
