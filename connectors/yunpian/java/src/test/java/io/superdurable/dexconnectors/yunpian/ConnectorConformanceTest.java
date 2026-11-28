// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yunpian;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/yunpian/scenarios.json",
                "https://sms.yunpian.com",
                "yunpian",
                "getAccountBalance",
                "sample-api-key",
                "getAccountBalance-defect",
                "getAccountBalance-oversized");
    }

    @org.junit.jupiter.api.Test
    void acceptsProxyPath() {
        org.junit.jupiter.api.Assertions.assertDoesNotThrow(() ->
                new AccountBalance.GetAccountBalanceOperation(
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
