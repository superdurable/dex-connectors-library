// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiling;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/weiling/scenarios.json",
                "https://openapi.weiling.cn",
                "weiling",
                "listDepartments",
                "REDACTED_APP_ID",
                "listDepartments-defect",
                "");
    }

    @org.junit.jupiter.api.Test
    void rejectsUntrustedOrigin() {
        org.junit.jupiter.api.Assertions.assertThrows(
                IllegalArgumentException.class,
                () ->
                        new DepartmentDirectory.ListDepartmentsOperation(
                                call -> null,
                                "https://example.org/prefix",
                                request -> {
                                    throw new AssertionError();
                                },
                                2097152));
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        DepartmentDirectory.ListDepartmentsOperation operation =
                new DepartmentDirectory.ListDepartmentsOperation(
                        c ->
                                new DepartmentDirectory.Credentials(
                                        credentials.resolve(c),
                                        new SecretString("REDACTED_APP_SECRET")),
                        "https://openapi.weiling.cn",
                        transport,
                        2 << 20);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                DepartmentDirectory.ListDepartmentsInput.class));
    }
}
