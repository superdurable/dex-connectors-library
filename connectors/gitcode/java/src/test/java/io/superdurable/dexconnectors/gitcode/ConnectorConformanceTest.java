// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.gitcode;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

final class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/gitcode/scenarios.json",
                "https://api.gitcode.com",
                "gitcode",
                "getRepository",
                "test-personal-access-token",
                "",
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
        return new GetRepositoryOperation(credentials, "https://api.gitcode.com", transport, responseLimit)
                .invoke(
                        call,
                        new ObjectMapper()
                                .treeToValue(scenario.get("input"), GetRepositoryInput.class));
    }
}
