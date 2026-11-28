// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.navinfo;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/navinfo/scenarios.json",
                "https://lbs.navinfo.com",
                "navinfo",
                "lookupIPLocation",
                "test-secret",
                "lookupIPLocation-missingCredential",
                "lookupIPLocation-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        IPLocation.LookupIPLocationOperation operation =
                new IPLocation.LookupIPLocationOperation(credentials, transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"), IPLocation.LookupIPLocationInput.class));
    }
}
