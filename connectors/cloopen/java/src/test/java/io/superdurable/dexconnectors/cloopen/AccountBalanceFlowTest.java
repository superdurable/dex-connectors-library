// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.cloopen;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.cloopen.AccountBalance.GetBalanceOperation;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

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
        GetBalanceOperation operation =
                new GetBalanceOperation(
                        call ->
                                new AccountBalance.Credentials(
                                        new SecretString("abcdefghijklmnopqrstuvwxyz012345"),
                                        new SecretString("012345abcdefghijklmnopqrstuvwxyz")),
                        "http://127.0.0.1:12345/",
                        request -> {
                            org.junit.jupiter.api.Assertions.assertTrue(
                                    request.getUrl()
                                            .startsWith(
                                                    "http://127.0.0.1:12345/2013-12-26/Accounts/abcdefghijklmnopqrstuvwxyz012345/AccountInfo?sig="));
                            return ProviderHttpResponse.buffered(
                                    calls.incrementAndGet() == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"statusCode\":\"000000\",\"Account\":{\"balance\":\"42.00\"}}"
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
                assertEquals("42.00", result.getSingleOutput(GetBalanceOutput.class).balance);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
