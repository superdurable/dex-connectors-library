// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.jingdigital;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/jingdigital/scenarios.json",
                "https://jingopenapi.jingsocial.com",
                "jingdigital",
                "listTags",
                "private-test-key",
                "listTags-defect",
                "listTags-oversized");
    }

    @org.junit.jupiter.api.Test
    void rejectsAnUndocumentedTokenOrigin() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new TagDirectory.ListTagsOperation(
                                call -> null,
                                "https://example.com",
                                request -> {
                                    throw new AssertionError();
                                },
                                262144));
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
                                scenario.get("name").asText().equals("listTags-invalidCredentials")
                                        ? null
                                        : new TagDirectory.Credentials(
                                                new SecretString("app-test"),
                                                new SecretString("secret-test")),
                        "https://jingopenapi.jingsocial.com",
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), TagDirectory.ListTagsInput.class));
    }
}
