// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiban;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/weiban/scenarios.json",
                "https://open.weibanzhushou.com",
                "weiban",
                "listTags",
                "private-test-key",
                "listTags-defect",
                "listTags-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        TagDirectory.ListTagsOperation operation =
                new TagDirectory.ListTagsOperation(
                        ignored ->
                                scenario.get("name").asText().equals("listTags-defect")
                                        ? null
                                        : new TagDirectory.Credentials(
                                                "corp_test",
                                                new SecretString("REDACTED_WEIBAN_SECRET")),
                        "https://open.weibanzhushou.com",
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), TagDirectory.ListTagsInput.class));
    }
}
