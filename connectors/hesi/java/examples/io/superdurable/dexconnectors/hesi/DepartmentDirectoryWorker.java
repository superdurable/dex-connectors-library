// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hesi;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.hesi.DepartmentDirectory.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the department lookup Flow with the static hesi connection. */
public final class DepartmentDirectoryWorker {
    private DepartmentDirectoryWorker() {}

    /** Start a Worker with the token supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        DepartmentDirectory.Credentials keys =
                new DepartmentDirectory.Credentials(
                        new SecretString(
                                java.util.Objects.requireNonNull(
                                        System.getenv("HESI_APP_KEY"), "HESI_APP_KEY is required")),
                        new SecretString(
                                java.util.Objects.requireNonNull(
                                        System.getenv("HESI_APP_SECURITY"),
                                        "HESI_APP_SECURITY is required")));
        DepartmentDirectoryFlow flow =
                new DepartmentDirectoryFlow(new ListDepartmentsOperation(call -> keys));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("department-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its department catalog. */
    public static final class DepartmentDirectoryFlow implements Flow<String> {
        private final QueryStep<
                        String, ListDepartmentsInput, ListDepartmentsOutput, ListDepartmentsResult>
                query;
        private final ReturnDepartments complete = new ReturnDepartments();

        /** Bind the operation during Worker registration. */
        public DepartmentDirectoryFlow(ListDepartmentsOperation operation) {
            query =
                    new QueryStep<
                            String,
                            ListDepartmentsInput,
                            ListDepartmentsOutput,
                            ListDepartmentsResult>(
                            DepartmentDirectory.newListDepartmentsStep(
                                            "ListDepartments",
                                            String.class,
                                            new StepAnnotations(
                                                    "department-directory",
                                                    "Department catalog",
                                                    "Read the available department catalog."),
                                            operation,
                                            new ConnectionRef("hesi", "hesi"),
                                            ignored -> new ListDepartmentsInput())
                                    .branch("listed", ReturnDepartments.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaHesiDepartmentDirectory";
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

    // dex:group group-id:department-directory group-label:"Department catalog"
    // dex:explanation text:"Complete with the provider's visible department catalog."
    public static final class ReturnDepartments implements Step<ListDepartmentsResult> {
        @Override
        public Class<ListDepartmentsResult> getInputType() {
            return ListDepartmentsResult.class;
        }

        @Override
        public StepDecision execute(Context context, ListDepartmentsResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
