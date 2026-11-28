// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sendcloud;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.sendcloud.AccountBalance.GetAccountBalanceOperation;

import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.time.Duration;
import java.util.Collections;
import java.util.UUID;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

@Tag("dex-dev")
class AccountBalanceFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        GetAccountBalanceOperation operation =
                new GetAccountBalanceOperation(
                        call ->
                                new AccountBalance.Credentials(
                                        new SecretString("app"), new SecretString("fixture-code")),
                        "http://127.0.0.1:12345/",
                        request -> {
                            assertEquals(
                                    "http://127.0.0.1:12345/apiv2/userinfo/get", request.getUrl());
                            return ProviderHttpResponse.buffered(
                                    calls.incrementAndGet() == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"statusCode\":200,\"result\":true,\"info\":{\"avaliableBalance\":4.50,\"balance\":\"7.20\",\"quota\":42,\"todayUsedQuota\":3}}"
                                            .getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        AccountBalanceWorker.AccountBalanceFlow flow =
                new AccountBalanceWorker.AccountBalanceFlow(operation);
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(new BlobCacheConfig(directory.toString(), 16L << 20));
                Client client = new Client(registry, cache, new ClientOptions(address))) {
            FutureTask<Void> serving;
            try (Worker worker =
                    new Worker(
                            registry,
                            cache,
                            WorkerOptions.newBuilder().serverAddress(address).build())) {
                serving =
                        new FutureTask<>(() -> {
                            worker.start();
                            return null;
                        });
                new Thread(serving, "account-balance-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-account-balance-" + UUID.randomUUID();
                client.startFlow(flow, flowId, "");
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                GetAccountBalanceOutput output =
                        result.getSingleOutput(GetAccountBalanceOutput.class);
                assertEquals("4.50", output.availableBalance);
                assertEquals("7.20", output.balance);
                assertEquals(42L, output.dailyQuota);
                assertEquals(3L, output.todayUsedQuota);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
