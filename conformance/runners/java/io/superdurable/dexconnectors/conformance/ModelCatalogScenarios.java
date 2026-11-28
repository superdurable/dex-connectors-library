// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.conformance;

import com.fasterxml.jackson.databind.JsonNode;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderHttpTransport;

/** Replay a provider's model catalog operation against Go recordings. */
public abstract class ModelCatalogScenarios extends QueryScenarios {
    protected ModelCatalogScenarios(String fixture, String origin, String connector, String key,
                                    String missingKeyCase, String smallBodyCase) {
        super(fixture, origin, connector, "listModels", key, missingKeyCase, smallBodyCase);
    }
    @Override protected final QueryAttempt<?> invokeScenario(Call call, JsonNode scenario, CredentialProvider<SecretString> credentials,
                                               ProviderHttpTransport transport, int limit) throws Exception {
        return invoke(call, scenario.get("input"), credentials, transport, limit);
    }

    protected abstract QueryAttempt<?> invoke(Call call, JsonNode input, CredentialProvider<SecretString> credentials,
                                               ProviderHttpTransport transport, int limit) throws Exception;
}
