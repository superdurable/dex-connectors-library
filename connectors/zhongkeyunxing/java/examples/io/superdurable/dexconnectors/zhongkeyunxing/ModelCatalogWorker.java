// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zhongkeyunxing;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.zhongkeyunxing.ModelCatalog.*;
import io.superdurable.dexconnectors.sdk.*;
import java.nio.file.Files;
import java.util.Collections;

/** Serves the catalog lookup Flow with the static zhongkeyunxing connection. */
public final class ModelCatalogWorker {
    private ModelCatalogWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("ZHONGKEYUNXING_API_KEY");
        if (key == null || key.isEmpty()) throw new IllegalStateException("ZHONGKEYUNXING_API_KEY is required");
        CatalogFlow flow = new CatalogFlow(new ListModelsOperation(call -> new SecretString(key)));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache = BlobCache.open(new BlobCacheConfig(Files.createTempDirectory("catalog-blobs").toString(), 16L << 20));
             Worker worker = new Worker(registry, cache, WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its model catalog. */
    public static final class CatalogFlow implements Flow<String> {
        private final QueryStep<String, ListModelsInput, ListModelsOutput, ListModelsResult> query;
        private final ReturnCatalog complete = new ReturnCatalog();

        /** Bind the operation during Worker registration. */
        public CatalogFlow(ListModelsOperation operation) {
            query = new QueryStep<String, ListModelsInput, ListModelsOutput, ListModelsResult>(
                    ModelCatalog.newListModelsStep("ListModels", String.class,
                            new StepAnnotations("model-catalog", "Model catalog", "Read the available model IDs."),
                            operation, new ConnectionRef("zhongkeyunxing", "zhongkeyunxing"), ignored -> new ListModelsInput())
                            .branch("found", ReturnCatalog.class).build()) {};
        }

        @Override public String getFlowType() { return "JavaZhongkeyunxingModelCatalog"; }
        @Override public StepList<String> getSteps() { return StepList.startStep(query).otherSteps(complete); }
        @Override public PersistenceSchema getPersistenceSchema() { return PersistenceSchema.of(); }
    }

    // dex:group group-id:model-catalog group-label:"Model catalog"
    // dex:explanation text:"Complete with the provider's visible model IDs."
    public static final class ReturnCatalog implements Step<ListModelsResult> {
        @Override public Class<ListModelsResult> getInputType() { return ListModelsResult.class; }
        @Override public StepDecision execute(Context context, ListModelsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
