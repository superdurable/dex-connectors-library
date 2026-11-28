// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.jisuapi;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.jisuapi.ProvinceDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the province lookup Flow with the static jisuapi connection. */
public final class ProvinceDirectoryWorker {
    private ProvinceDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("JISUAPI_APPKEY"), "JISUAPI_APPKEY is required"));
        ProvinceDirectoryFlow flow =
                new ProvinceDirectoryFlow(new ListProvincesOperation(call -> token));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("province-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its province catalog. */
    public static final class ProvinceDirectoryFlow implements Flow<ListProvincesInput> {
        private final QueryStep<
                        ListProvincesInput,
                        ListProvincesInput,
                        ListProvincesOutput,
                        ListProvincesResult>
                query;
        private final ReturnProvinces complete = new ReturnProvinces();

        /** Bind the operation during Worker registration. */
        public ProvinceDirectoryFlow(ListProvincesOperation operation) {
            query =
                    new QueryStep<
                            ListProvincesInput,
                            ListProvincesInput,
                            ListProvincesOutput,
                            ListProvincesResult>(
                            ProvinceDirectory.newListProvincesStep(
                                            "ListProvinces",
                                            ListProvincesInput.class,
                                            new StepAnnotations(
                                                    "province-directory",
                                                    "Province catalog",
                                                    "Read the available province catalog."),
                                            operation,
                                            new ConnectionRef("jisuapi", "jisuapi"),
                                            input -> input)
                                    .branch("listed", ReturnProvinces.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaJisuAPIProvinceDirectory";
        }

        @Override
        public StepList<ListProvincesInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:province-directory group-label:"Province catalog"
    // dex:explanation text:"Complete with the provider's visible province catalog."
    public static final class ReturnProvinces implements Step<ListProvincesResult> {
        @Override
        public Class<ListProvincesResult> getInputType() {
            return ListProvincesResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListProvincesResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
