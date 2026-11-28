// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.ipdatacloud;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/ipdatacloud/scenarios.json",
                "https://api.ipdatacloud.com",
                "ipdatacloud",
                "lookupIPLocation",
                "private-test-key",
                null,
                "lookupIPLocation-oversized");
    }

    @Override
    protected QueryAttempt<?> invoke(
            Call call,
            JsonNode input,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        IPLocation.LookupIPLocationOperation operation =
                new IPLocation.LookupIPLocationOperation(credentials, transport, limit);
        return operation.invoke(
                call,
                new ObjectMapper().treeToValue(input, IPLocation.LookupIPLocationInput.class));
    }
}
