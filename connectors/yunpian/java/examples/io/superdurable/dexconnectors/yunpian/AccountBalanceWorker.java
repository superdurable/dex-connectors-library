// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.yunpian;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.*;
import io.superdurable.dexconnectors.yunpian.AccountBalance.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the balance lookup Flow with the static yunpian connection. */
public final class AccountBalanceWorker {
    private AccountBalanceWorker() {}

    /** Start a Worker with the API key supplied through its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("YUNPIAN_API_KEY");
        if (key == null || key.isEmpty())
            throw new IllegalStateException("YUNPIAN_API_KEY is required");
        AccountBalanceFlow flow =
                new AccountBalanceFlow(
                        new GetAccountBalanceOperation(call -> new SecretString(key)));
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("balance-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }

    /** One provider query followed by completion with its account balance. */
    public static final class AccountBalanceFlow implements Flow<String> {
        private final QueryStep<
                        String,
                        GetAccountBalanceInput,
                        GetAccountBalanceOutput,
                        GetAccountBalanceResult>
                query;
        private final ReturnBalance complete = new ReturnBalance();

        /** Bind the operation during Worker registration. */
        public AccountBalanceFlow(GetAccountBalanceOperation operation) {
            query =
                    new QueryStep<
                            String,
                            GetAccountBalanceInput,
                            GetAccountBalanceOutput,
                            GetAccountBalanceResult>(
                            AccountBalance.newGetAccountBalanceStep(
                                            "GetAccountBalance",
                                            String.class,
                                            new StepAnnotations(
                                                    "account-balance",
                                                    "Account balance",
                                                    "Read the available account balance."),
                                            operation,
                                            new ConnectionRef("yunpian", "yunpian"),
                                            ignored -> new GetAccountBalanceInput())
                                    .branch("read", ReturnBalance.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaYunpianAccountBalance";
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

    // dex:group group-id:account-balance group-label:"Account balance"
    // dex:explanation text:"Complete with the provider's visible account balance."
    public static final class ReturnBalance implements Step<GetAccountBalanceResult> {
        @Override
        public Class<GetAccountBalanceResult> getInputType() {
            return GetAccountBalanceResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetAccountBalanceResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
