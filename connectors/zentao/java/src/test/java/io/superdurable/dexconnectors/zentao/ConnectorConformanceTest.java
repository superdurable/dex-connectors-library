// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zentao;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/zentao/scenarios.json",
                "https://zentao.example.org/zentao",
                "zentao",
                "listProducts",
                "secretToken",
                "listProducts-defect",
                "listProducts-oversized");
    }

    @org.junit.jupiter.api.Test
    void requiresInstallationRoot() {
        for (String root :
                new String[] {
                    "",
                    "https://example.org/?q=invalid",
                    "https://example.org/zentao/api.php",
                    "https://example.org/zentao/api.php/v1"
                }) {
            org.junit.jupiter.api.Assertions.assertThrows(
                    IllegalArgumentException.class,
                    () -> new ProductDirectory.ListProductsOperation(call -> null, root));
        }
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        ProductDirectory.ListProductsOperation operation =
                new ProductDirectory.ListProductsOperation(
                        credentials, "https://zentao.example.org/zentao", transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"), ProductDirectory.ListProductsInput.class));
    }
}
