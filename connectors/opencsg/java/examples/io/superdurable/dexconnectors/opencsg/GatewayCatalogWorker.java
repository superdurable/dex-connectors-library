// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.opencsg;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.opencsg.GatewayCatalog.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the application lookup Flow with the static opencsg connection. */
public final class GatewayCatalogWorker {
    private GatewayCatalogWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("OPENCSG_API_KEY"), "OPENCSG_API_KEY is required"));
        GatewayCatalogFlow flow =
                new GatewayCatalogFlow(new ListGatewayModelsOperation(call -> token));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("gateway-catalog-blobs")
                                                .toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its model catalog. */
    public static final class GatewayCatalogFlow implements Flow<ListGatewayModelsInput> {
        private final QueryStep<
                        ListGatewayModelsInput,
                        ListGatewayModelsInput,
                        ListGatewayModelsOutput,
                        ListGatewayModelsResult>
                query;
        private final ReturnModels complete = new ReturnModels();

        /** Bind the operation during Worker registration. */
        public GatewayCatalogFlow(ListGatewayModelsOperation operation) {
            query =
                    new QueryStep<
                            ListGatewayModelsInput,
                            ListGatewayModelsInput,
                            ListGatewayModelsOutput,
                            ListGatewayModelsResult>(
                            GatewayCatalog.newListGatewayModelsStep(
                                            "ListGatewayModels",
                                            ListGatewayModelsInput.class,
                                            new StepAnnotations(
                                                    "gateway-catalog",
                                                    "Gateway catalog",
                                                    "Read the available model catalog."),
                                            operation,
                                            new ConnectionRef("opencsg", "opencsg"),
                                            input -> input)
                                    .branch("found", ReturnModels.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaOpenCSGGatewayCatalog";
        }

        @Override
        public StepList<ListGatewayModelsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:gateway-catalog group-label:"Gateway catalog"
    // dex:explanation text:"Complete with the provider's visible model catalog."
    public static final class ReturnModels implements Step<ListGatewayModelsResult> {
        @Override
        public Class<ListGatewayModelsResult> getInputType() {
            return ListGatewayModelsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListGatewayModelsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
