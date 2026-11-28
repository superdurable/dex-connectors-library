// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.dieyuyun;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/dieyuyun/scenarios.json",
                "https://open.dieyuyun.com",
                "dieyuyun",
                "getAccountBalance",
                "private-test-key",
                "getAccountBalance-missing-key",
                "getAccountBalance-oversize");
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
                                65536));
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        if (scenario.get("name").asText().endsWith("-oversize")) limit = 8;
        AccountBalance.GetAccountBalanceOperation operation =
                new AccountBalance.GetAccountBalanceOperation(credentials, transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                AccountBalance.GetAccountBalanceInput.class));
    }
}
