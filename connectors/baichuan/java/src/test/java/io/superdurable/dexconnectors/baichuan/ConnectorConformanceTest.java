// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.baichuan;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/baichuan/scenarios.json",
                "https://api.baichuan-ai.com",
                "baichuan",
                "listKnowledgeBases",
                "test-baichuan-key-123",
                "listKnowledgeBases-defect",
                "listKnowledgeBases-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        if (scenario.get("name").asText().endsWith("-oversized")) limit = 30;
        KnowledgeBaseDirectory.ListKnowledgeBasesOperation operation =
                new KnowledgeBaseDirectory.ListKnowledgeBasesOperation(
                        credentials, transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                KnowledgeBaseDirectory.ListKnowledgeBasesInput.class));
    }
}
