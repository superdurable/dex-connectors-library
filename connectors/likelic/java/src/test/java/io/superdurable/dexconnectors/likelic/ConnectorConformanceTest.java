// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.likelic;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/likelic/scenarios.json",
                "https://agent.likelic.com",
                "likelic",
                "listRooms",
                "sk-0123456789abcdef0123456789abcdef0123456789abcdef",
                "listRooms-invalidCredentials",
                "listRooms-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        RoomDirectory.ListRoomsOperation operation =
                new RoomDirectory.ListRoomsOperation(
                        c -> new RoomDirectory.Credentials("demo_company", credentials.resolve(c)),
                        transport,
                        limit);
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(scenario.get("input"), RoomDirectory.ListRoomsInput.class));
    }
}
