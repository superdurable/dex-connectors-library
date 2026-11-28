// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ctyun;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/ctyun/scenarios.json",
                "https://kqa-global.ctapi.ctyun.cn",
                "ctyun",
                "listKnowledgeBases",
                "AKIDEXAMPLE",
                "listKnowledgeBases-missing-credentials",
                "listKnowledgeBases-oversize");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        if (scenario.get("name").asText().endsWith("-oversize")) limit = 4;
        KnowledgeBaseDirectory.ListKnowledgeBasesOperation operation =
                new KnowledgeBaseDirectory.ListKnowledgeBasesOperation(
                        c ->
                                new KnowledgeBaseDirectory.Credentials(
                                        credentials.resolve(c),
                                        new SecretString("SKEXAMPLE"),
                                        new SecretString("tenant-example")),
                        "https://kqa-global.ctapi.ctyun.cn",
                        transport,
                        limit,
                        java.time.Clock.fixed(
                                java.time.Instant.parse(scenario.get("fixedClock").asText()),
                                java.time.ZoneOffset.UTC),
                        () -> {
                            if (scenario.get("name").asText().endsWith("-id-generation-failure"))
                                throw new IllegalStateException();
                            return "123e4567-e89b-42d3-a456-426614174000";
                        });
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                KnowledgeBaseDirectory.ListKnowledgeBasesInput.class));
    }
}
