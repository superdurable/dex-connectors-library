// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.textin;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/textin/scenarios.json",
                "https://api.textin.com",
                "textin",
                "getAccountBalance",
                "secret-test-code",
                "getAccountBalance-defect",
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
        limit = 65536;
        AccountBalance.GetAccountBalanceOperation operation =
                new AccountBalance.GetAccountBalanceOperation(
                        c ->
                                new AccountBalance.Credentials(
                                        scenario.get("name").asText().endsWith("-defect")
                                                ? ""
                                                : "test-app-id",
                                        new SecretString("secret-test-code")),
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
