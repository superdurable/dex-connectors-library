// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.chukou1;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/chukou1/scenarios.json",
                "https://openapi.chukou1.cn",
                "chukou1",
                "listWarehouses",
                "secretTestToken",
                "listWarehouses-missing-credentials",
                "listWarehouses-oversize");
    }

    @org.junit.jupiter.api.Test
    void acceptsProxyPath() {
        org.junit.jupiter.api.Assertions.assertDoesNotThrow(() ->
                new WarehouseCatalog.ListWarehousesOperation(
                        call -> null,
                        "https://example.org/prefix",
                        request -> {
                            throw new AssertionError();
                        },
                        1048576));
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        WarehouseCatalog.ListWarehousesOperation operation =
                new WarehouseCatalog.ListWarehousesOperation(
                        credentials,
                        "https://openapi.chukou1.cn",
                        transport,
                        scenario.get("name").asText().endsWith("-oversize") ? 4 : limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"), WarehouseCatalog.ListWarehousesInput.class));
    }
}
