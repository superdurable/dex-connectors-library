// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yanwen;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.yanwen.WarehouseCatalog.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the warehouse lookup Flow with the static yanwen connection. */
public final class WarehouseCatalogWorker {
    private WarehouseCatalogWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        WarehouseCatalog.Credentials credentials =
                new WarehouseCatalog.Credentials(
                        requireEnvironment("YANWEN_USER_ID"),
                        new SecretString(requireEnvironment("YANWEN_API_TOKEN")));
        WarehouseCatalogFlow flow =
                new WarehouseCatalogFlow(new ListWarehousesOperation(call -> credentials));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("warehouse-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    private static String requireEnvironment(String name) {
        String value = System.getenv(name);
        if (value == null || value.isEmpty())
            throw new IllegalStateException(name + " is required");
        return value;
    }

    /** One provider query followed by completion with its warehouse catalog. */
    public static final class WarehouseCatalogFlow implements Flow<String> {
        private final QueryStep<String, ListWarehousesInput, WarehouseList, ListWarehousesResult>
                query;
        private final ReturnWarehouses complete = new ReturnWarehouses();

        /** Bind the operation during Worker registration. */
        public WarehouseCatalogFlow(ListWarehousesOperation operation) {
            query =
                    new QueryStep<String, ListWarehousesInput, WarehouseList, ListWarehousesResult>(
                            WarehouseCatalog.newListWarehousesStep(
                                            "ListWarehouses",
                                            String.class,
                                            new StepAnnotations(
                                                    "warehouse-catalog",
                                                    "Warehouse catalog",
                                                    "Read the available warehouse catalog."),
                                            operation,
                                            new ConnectionRef("yanwen", "yanwen"),
                                            ignored -> new ListWarehousesInput())
                                    .branch("listed", ReturnWarehouses.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaYanwenWarehouseCatalog";
        }

        @Override
        public StepList<String> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:warehouse-catalog group-label:"Warehouse catalog"
    // dex:explanation text:"Complete with the provider's visible warehouse catalog."
    public static final class ReturnWarehouses implements Step<ListWarehousesResult> {
        @Override
        public Class<ListWarehousesResult> getInputType() {
            return ListWarehousesResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListWarehousesResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
