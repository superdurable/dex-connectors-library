// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.chuanglan;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/chuanglan/scenarios.json",
                "https://smssh1.253.com",
                "chuanglan",
                "getBalance",
                "secret-password",
                "getBalance-missing-credentials",
                "getBalance-oversize");
    }

    @org.junit.jupiter.api.Test
    void requiresHostRoot() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new AccountBalance.GetBalanceOperation(
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
        limit = scenario.get("name").asText().endsWith("-oversize") ? 4 : 65536;
        AccountBalance.GetBalanceOperation operation =
                new AccountBalance.GetBalanceOperation(
                        c ->
                                new AccountBalance.Credentials(
                                        new SecretString(
                                                scenario.get("name")
                                                                .asText()
                                                                .endsWith("-missing-credentials")
                                                        ? ""
                                                        : "N9000123"),
                                        new SecretString("secret-password")),
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), AccountBalance.GetBalanceInput.class));
    }
}
