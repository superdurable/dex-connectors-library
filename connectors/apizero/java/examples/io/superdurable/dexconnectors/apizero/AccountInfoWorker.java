// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.apizero;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.apizero.AccountInfo.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the account lookup Flow with the static apizero connection. */
public final class AccountInfoWorker {
    private AccountInfoWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("APIZERO_API_KEY");
        if (key == null || key.isEmpty())
            throw new IllegalStateException("APIZERO_API_KEY is required");
        AccountInfoFlow flow =
                new AccountInfoFlow(new GetAccountInfoOperation(call -> new SecretString(key)));
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

    /** One provider query followed by completion with its account information. */
    public static final class AccountInfoFlow implements Flow<String> {
        private final QueryStep<
                        String, GetAccountInfoInput, GetAccountInfoOutput, GetAccountInfoResult>
                query;
        private final ReturnAccount complete = new ReturnAccount();

        /** Bind the operation during Worker registration. */
        public AccountInfoFlow(GetAccountInfoOperation operation) {
            query =
                    new QueryStep<
                            String,
                            GetAccountInfoInput,
                            GetAccountInfoOutput,
                            GetAccountInfoResult>(
                            AccountInfo.newGetAccountInfoStep(
                                            "GetAccountInfo",
                                            String.class,
                                            new StepAnnotations(
                                                    "account-info",
                                                    "Account information",
                                                    "Read wallet or prepaid quota information."),
                                            operation,
                                            new ConnectionRef("apizero", "apizero"),
                                            ignored -> new GetAccountInfoInput())
                                    .branch("found", ReturnAccount.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaApiZeroAccountInfo";
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

    // dex:group group-id:account-info group-label:"Account information"
    // dex:explanation text:"Complete with the provider's visible account information."
    public static final class ReturnAccount implements Step<GetAccountInfoResult> {
        @Override
        public Class<GetAccountInfoResult> getInputType() {
            return GetAccountInfoResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetAccountInfoResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
