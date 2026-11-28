// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.wapi;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.wapi.VehicleBrandDirectory.*;
import io.superdurable.dexconnectors.wapi.VehicleBrandDirectory.Credentials;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the vehicle brand lookup Flow with the static wapi connection. */
public final class VehicleBrandDirectoryWorker {
    private VehicleBrandDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        SecretString token =
                new SecretString(
                        java.util.Objects.requireNonNull(
                                System.getenv("WAPI_APP_SECRET"), "WAPI_APP_SECRET is required"));
        Credentials credentials =
                new Credentials(
                        java.util.Objects.requireNonNull(
                                System.getenv("WAPI_APP_ID"), "WAPI_APP_ID is required"),
                        token);
        VehicleBrandDirectoryFlow flow =
                new VehicleBrandDirectoryFlow(new ListVehicleBrandsOperation(call -> credentials));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("vehicle-brand-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its vehicle brand catalog. */
    public static final class VehicleBrandDirectoryFlow implements Flow<ListVehicleBrandsInput> {
        private final QueryStep<
                        ListVehicleBrandsInput,
                        ListVehicleBrandsInput,
                        ListVehicleBrandsOutput,
                        ListVehicleBrandsResult>
                query;
        private final ReturnBrands complete = new ReturnBrands();

        /** Bind the operation during Worker registration. */
        public VehicleBrandDirectoryFlow(ListVehicleBrandsOperation operation) {
            query =
                    new QueryStep<
                            ListVehicleBrandsInput,
                            ListVehicleBrandsInput,
                            ListVehicleBrandsOutput,
                            ListVehicleBrandsResult>(
                            VehicleBrandDirectory.newListVehicleBrandsStep(
                                            "ListVehicleBrands",
                                            ListVehicleBrandsInput.class,
                                            new StepAnnotations(
                                                    "vehicle-brand-directory",
                                                    "VehicleBrand catalog",
                                                    "Read the available vehicle brand catalog."),
                                            operation,
                                            new ConnectionRef("wapi", "wapi"),
                                            input -> input)
                                    .branch("found", ReturnBrands.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaWapiVehicleBrandDirectory";
        }

        @Override
        public StepList<ListVehicleBrandsInput> getSteps() {
            return StepList.startStep(query).otherSteps(complete);
        }

        @Override
        public PersistenceSchema getPersistenceSchema() {
            return PersistenceSchema.of();
        }
    }

    // dex:group group-id:vehicle-brand-directory group-label:"VehicleBrand catalog"
    // dex:explanation text:"Complete with the provider's visible vehicle brand catalog."
    public static final class ReturnBrands implements Step<ListVehicleBrandsResult> {
        @Override
        public Class<ListVehicleBrandsResult> getInputType() {
            return ListVehicleBrandsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListVehicleBrandsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
