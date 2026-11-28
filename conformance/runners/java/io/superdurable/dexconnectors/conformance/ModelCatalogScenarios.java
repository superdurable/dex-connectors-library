// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.conformance;

/** Replay a provider's model catalog operation against Go recordings. */
public abstract class ModelCatalogScenarios extends QueryScenarios {
    protected ModelCatalogScenarios(String fixture, String origin, String connector, String key,
                                    String missingKeyCase, String smallBodyCase) {
        super(fixture, origin, connector, "listModels", key, missingKeyCase, smallBodyCase);
    }
}
