// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.wapi;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import io.superdurable.dexconnectors.conformance.QueryScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends QueryScenarios {
    ConnectorConformanceTest() {
        super(
                "../../../conformance/connectors/wapi/scenarios.json",
                "https://uaqy.api.storeapi.net",
                "wapi",
                "listVehicleBrands",
                "REDACTEDAPPSECRET000000000000000",
                "listVehicleBrands-defect",
                "listVehicleBrands-oversized");
    }

    @Override
    protected QueryAttempt<?> invokeScenario(
            Call call,
            JsonNode scenario,
            CredentialProvider<SecretString> credentials,
            ProviderHttpTransport transport,
            int limit)
            throws Exception {
        VehicleBrandDirectory.ListVehicleBrandsOperation operation =
                new VehicleBrandDirectory.ListVehicleBrandsOperation(
                        c -> new VehicleBrandDirectory.Credentials("123", credentials.resolve(c)),
                        transport,
                        limit,
                        java.time.Clock.fixed(
                                java.time.Instant.parse(scenario.get("fixedClock").textValue()),
                                java.time.ZoneOffset.UTC));
        return operation.invoke(
                call,
                new ObjectMapper()
                        .treeToValue(
                                scenario.get("input"),
                                VehicleBrandDirectory.ListVehicleBrandsInput.class));
    }
}
