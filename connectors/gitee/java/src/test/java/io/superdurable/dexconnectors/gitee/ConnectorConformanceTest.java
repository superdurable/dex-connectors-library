// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitee;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

final class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/gitee/scenarios.json",
                "https://gitee.com",
                "gitee",
                "getRepository",
                "test-gitee-token",
                "getRepository-defect",
                "getRepository-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        int responseLimit = scenario.path("name").asText().endsWith("-smallLimit") ? 16 : limit;
        return new GetRepositoryOperation(credentials, "https://gitee.com", transport, responseLimit)
                .invoke(
                        call,
                        new ObjectMapper()
                                .treeToValue(scenario.get("input"), GetRepositoryInput.class));
    }
}
