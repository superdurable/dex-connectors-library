// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yijingtong;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/yijingtong/scenarios.json",
                "https://tenant.example",
                "yijingtong",
                "listWarehouses",
                "REDACTED_YIJINGTONG_TOKEN",
                "listWarehouses-defect",
                "listWarehouses-oversized");
    }

    @org.junit.jupiter.api.Test
    void rejectsTenantUrlWithPath() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
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
                        credentials, "https://tenant.example", transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"), WarehouseCatalog.ListWarehousesInput.class));
    }
}
