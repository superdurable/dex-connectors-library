// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.dimleap;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.superdurable.dexconnectors.conformance.ModelCatalogScenarios;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

class ConnectorConformanceTest extends ModelCatalogScenarios {
    ConnectorConformanceTest() {
        super("../../../conformance/connectors/dimleap/scenarios.json", "https://api.dimleap.cn", "dimleap", "sk-test-secret",
                "listModels-missing-credentials", "listModels-oversize");
    }

    @Override protected QueryAttempt<?> invoke(Call call, JsonNode input, CredentialProvider<SecretString> credentials,
                                               ProviderHttpTransport transport, int limit) throws Exception {
        ModelCatalog.ListModelsOperation operation = new ModelCatalog.ListModelsOperation(credentials, transport, limit);
        return operation.invoke(call, new ObjectMapper().treeToValue(input, ModelCatalog.ListModelsInput.class));
    }
}
