// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.worktile;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/worktile/scenarios.json",
                "https://dev.worktile.com",
                "worktile",
                "listProjects",
                "private-test-key",
                "listProjects-defect",
                "listProjects-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        ProjectDirectory.ListProjectsOperation operation =
                new ProjectDirectory.ListProjectsOperation(
                        ignored ->
                                scenario.get("name").asText().equals("listProjects-defect")
                                        ? null
                                        : new ProjectDirectory.Credentials(
                                                new SecretString("REDACTED_WORKTILE_CLIENT_ID"),
                                                new SecretString(
                                                        "REDACTED_WORKTILE_CLIENT_SECRET")),
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"), ProjectDirectory.ListProjectsInput.class));
    }
}
