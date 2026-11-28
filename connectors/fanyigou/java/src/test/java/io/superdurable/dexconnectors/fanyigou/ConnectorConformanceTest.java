// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.fanyigou;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/fanyigou/scenarios.json",
                "https://www.fanyigou.com",
                "fanyigou",
                "getAccountBalance",
                "secret-private_key",
                "getAccountBalance-defect",
                "getAccountBalance-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        boolean missing = scenario.get("name").asText().endsWith("-defect");
        String seed = scenario.path("randomSeed").asText();
        byte[] block =
                java.security.MessageDigest.getInstance("SHA-256")
                        .digest(
                                java.util.Arrays.copyOf(
                                        seed.getBytes(java.nio.charset.StandardCharsets.UTF_8),
                                        seed.getBytes(java.nio.charset.StandardCharsets.UTF_8)
                                                        .length
                                                + 8));
        AccountBalance.GetAccountBalanceOperation operation =
                new AccountBalance.GetAccountBalanceOperation(
                        c ->
                                new AccountBalance.Credentials(
                                        new SecretString(missing ? "" : "test-app"),
                                        new SecretString("test-private-key")),
                        transport,
                        limit,
                        () ->
                                scenario.get("name").asText().endsWith("-nonceUnavailable")
                                        ? null
                                        : java.util.Arrays.copyOf(block, 16));
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                AccountBalance.GetAccountBalanceInput.class));
    }
}
