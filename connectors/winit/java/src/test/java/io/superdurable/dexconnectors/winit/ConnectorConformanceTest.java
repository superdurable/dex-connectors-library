// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.winit;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/winit/scenarios.json",
                "https://openapi.winit.com.cn",
                "winit",
                "listWarehouses",
                "REDACTED_TOKEN",
                "listWarehouses-defect",
                "listWarehouses-oversized");
    }

    @org.junit.jupiter.api.Test
    void acceptsProxyPath() {
        org.junit.jupiter.api.Assertions.assertDoesNotThrow(() ->
                new WarehouseCatalog.ListWarehousesOperation(
                        call -> null,
                        "https://example.org/prefix",
                        "OWNERERP",
                        request -> {
                            throw new AssertionError();
                        },
                        1048576,
                        java.time.Clock.systemUTC()));
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
                        c ->
                                new WarehouseCatalog.Credentials(
                                        new SecretString("REDACTED_APP_KEY"),
                                        new SecretString(credentials.resolve(c).reveal()),
                                        new SecretString("REDACTED_CLIENT_ID"),
                                        new SecretString("REDACTED_CLIENT_SECRET")),
                        "https://openapi.winit.com.cn",
                        "OWNERERP",
                        transport,
                        limit,
                        java.time.Clock.fixed(
                                java.time.Instant.parse(
                                        scenario.path("fixedClock").asText("2000-01-01T00:00:00Z")),
                                java.time.ZoneOffset.UTC));
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"), WarehouseCatalog.ListWarehousesInput.class));
    }
}
