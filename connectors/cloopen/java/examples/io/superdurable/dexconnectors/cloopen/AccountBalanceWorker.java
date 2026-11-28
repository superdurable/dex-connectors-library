// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.cloopen;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.cloopen.AccountBalance.*;
import io.superdurable.dexconnectors.sdk.*;

import java.nio.file.Files;
import java.util.Collections;

/** Serves the balance lookup Flow with the static cloopen connection. */
public final class AccountBalanceWorker {
    private AccountBalanceWorker() {}

    /** Start a Worker with the account SID and auth token from its process environment. */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String key = System.getenv("CLOOPEN_AUTH_TOKEN");
        String account = System.getenv("CLOOPEN_ACCOUNT_SID");
        if (key == null || key.isEmpty() || account == null || account.isEmpty())
            throw new IllegalStateException(
                    "CLOOPEN_ACCOUNT_SID and CLOOPEN_AUTH_TOKEN are required");
        AccountBalanceFlow flow =
                new AccountBalanceFlow(
                        new GetBalanceOperation(call ->
                                new AccountBalance.Credentials(
                                        new SecretString(account), new SecretString(key))));
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
        private final QueryStep<String, GetBalanceInput, GetBalanceOutput, GetBalanceResult> query;
        private final ReturnBalance complete = new ReturnBalance();

        /** Bind the operation during Worker registration. */
        public AccountBalanceFlow(GetBalanceOperation operation) {
            query =
                    new QueryStep<String, GetBalanceInput, GetBalanceOutput, GetBalanceResult>(
                            AccountBalance.newGetBalanceStep(
                                            "GetBalance",
                                            String.class,
                                            new StepAnnotations(
                                                    "account-balance",
                                                    "Account balance",
                                                    "Read the available account balance."),
                                            operation,
                                            new ConnectionRef("cloopen", "cloopen"),
                                            ignored -> new GetBalanceInput())
                                    .branch("fetched", ReturnBalance.class)
                                    .build()) {};
        }

        @Override
        public String getFlowType() {
            return "JavaCloopenAccountBalance";
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
    public static final class ReturnBalance implements Step<GetBalanceResult> {
        @Override
        public Class<GetBalanceResult> getInputType() {
            return GetBalanceResult.class;
        }

        @Override
        public StepDecision execute(Context context, GetBalanceResult result) {
            return StepDecision.gracefulComplete(result.getValue());
        }
    }
}
