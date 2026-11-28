// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sendcloud;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/sendcloud/scenarios.json",
                "https://api.sendcloud.net",
                "sendcloud",
                "getAccountBalance",
                "test-api-key",
                "getAccountBalance-defect",
                "getAccountBalance-oversized");
    }

    @org.junit.jupiter.api.Test
    void requiresHostRoot() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new AccountBalance.GetAccountBalanceOperation(
                                call -> null,
                                "https://example.org/prefix",
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
        limit = scenario.get("name").asText().endsWith("-oversized") ? 8 : 262144;
        AccountBalance.GetAccountBalanceOperation operation =
                new AccountBalance.GetAccountBalanceOperation(
                        c ->
                                new AccountBalance.Credentials(
                                        new SecretString(
                                                scenario.get("name").asText().endsWith("-defect")
                                                        ? ""
                                                        : "test-api-user"),
                                        new SecretString("test-api-key")),
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                AccountBalance.GetAccountBalanceInput.class));
    }
}
