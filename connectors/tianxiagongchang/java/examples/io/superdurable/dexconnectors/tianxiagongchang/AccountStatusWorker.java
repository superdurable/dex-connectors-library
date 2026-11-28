// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.tianxiagongchang;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.tianxiagongchang.AccountStatus.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the account lookup Flow with the static tianxiagongchang connection. */
public final class AccountStatusWorker {
    private AccountStatusWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("TIANXIAGONGCHANG_API_KEY");
        if (key == null || key.isEmpty())
            throw new IllegalStateException("TIANXIAGONGCHANG_API_KEY is required");
        AccountStatusFlow flow =
                new AccountStatusFlow(new GetAccountStatusOperation(call -> new SecretString(key)));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("account-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its account status. */
    public static final class AccountStatusFlow implements Flow<String> {
        private final QueryStep<
                        String,
                        GetAccountStatusInput,
                        GetAccountStatusOutput,
                        GetAccountStatusResult>
                query;
        private final ReturnAccount complete = new ReturnAccount();

        /** Bind the operation during Worker registration. */
        public AccountStatusFlow(GetAccountStatusOperation operation) {
            query =
                    new QueryStep<
                            String,
                            GetAccountStatusInput,
                            GetAccountStatusOutput,
                            GetAccountStatusResult>(
                            AccountStatus.newGetAccountStatusStep(
                                            "GetAccountStatus",
                                            String.class,
                                            new StepAnnotations(
                                                    "account-status",
                                                    "Account status",
                                                    "Read the connected account’s quota and"
                                                        + " status."),
                                            operation,
                                            new ConnectionRef(
                                                    "tianxiagongchang", "tianxiagongchang"),
                                            ignored -> new GetAccountStatusInput())
                                    .branch("retrieved", ReturnAccount.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaTianxiaGongchangAccountStatus";
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

    // dex:group group-id:account-status group-label:"Account status"
    // dex:explanation text:"Complete with the provider's visible account status."
    public static final class ReturnAccount implements Step<GetAccountStatusResult> {
        @Override
        public Class<GetAccountStatusResult> getInputType() {
            return GetAccountStatusResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetAccountStatusResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
