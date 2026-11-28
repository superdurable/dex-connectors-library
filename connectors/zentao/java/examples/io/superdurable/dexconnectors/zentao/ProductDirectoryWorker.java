// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zentao;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.zentao.ProductDirectory.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the product lookup Flow with the static zentao connection. */
public final class ProductDirectoryWorker {
    private ProductDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("ZENTAO_TOKEN"), "ZENTAO_TOKEN is required"));
        String origin =
                java.util.Objects.requireNonNull(
                        System.getenv("ZENTAO_BASE_URL"), "ZENTAO_BASE_URL is required");
        ProductDirectoryFlow flow =
                new ProductDirectoryFlow(new ListProductsOperation(call -> token, origin));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("product-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its product catalog. */
    public static final class ProductDirectoryFlow implements Flow<ListProductsInput> {
        private final QueryStep<
                        ListProductsInput, ListProductsInput, ProductList, ListProductsResult>
                query;
        private final ReturnProducts complete = new ReturnProducts();

        /** Bind the operation during Worker registration. */
        public ProductDirectoryFlow(ListProductsOperation operation) {
            query =
                    new QueryStep<
                            ListProductsInput, ListProductsInput, ProductList, ListProductsResult>(
                            ProductDirectory.newListProductsStep(
                                            "ListProducts",
                                            ListProductsInput.class,
                                            new StepAnnotations(
                                                    "product-directory",
                                                    "Product catalog",
                                                    "Read the available product catalog."),
                                            operation,
                                            new ConnectionRef("zentao", "zentao"),
                                            input -> input)
                                    .branch("listed", ReturnProducts.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaZenTaoProductDirectory";
        }

        @Override
        public StepList<ListProductsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:product-directory group-label:"Product catalog"
    // dex:explanation text:"Complete with the provider's visible product catalog."
    public static final class ReturnProducts implements Step<ListProductsResult> {
        @Override
        public Class<ListProductsResult> getInputType() {
            return ListProductsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListProductsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
