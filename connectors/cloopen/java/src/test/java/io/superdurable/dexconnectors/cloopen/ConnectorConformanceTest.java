// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.cloopen;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/cloopen/scenarios.json",
                "https://app.cloopen.com:8883",
                "cloopen",
                "getBalance",
                "012345abcdefghijklmnopqrstuvwxyz",
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
                                                        : "abcdefghijklmnopqrstuvwxyz012345"),
                                        new SecretString("012345abcdefghijklmnopqrstuvwxyz")),
                        "https://app.cloopen.com:8883",
                        transport,
                        limit,
                        scenario.has("fixedClock")
                                ? java.time.Clock.fixed(
                                        java.time.Instant.parse(
                                                scenario.get("fixedClock").asText()),
                                        java.time.ZoneOffset.UTC)
                                : java.time.Clock.systemUTC());
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), AccountBalance.GetBalanceInput.class));
    }
}
