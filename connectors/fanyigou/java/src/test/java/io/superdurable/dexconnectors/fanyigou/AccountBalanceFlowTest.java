// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.fanyigou;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.fanyigou.AccountBalance.GetAccountBalanceOperation;
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
        java.util.Set<String> nonces = java.util.concurrent.ConcurrentHashMap.newKeySet();
        AtomicInteger calls = new AtomicInteger();
        GetAccountBalanceOperation operation =
                new GetAccountBalanceOperation(
                        call ->
                                new AccountBalance.Credentials(
                                        new SecretString("app"), new SecretString("fixture-code")),
                        request -> {
                            assertEquals(
                                    "https://www.fanyigou.com/TranslateApi/api/getAccount",
                                    request.getUrl());
                            String form = new String(request.getBody(), StandardCharsets.UTF_8);
                            java.util.regex.Matcher nonce =
                                    java.util.regex.Pattern.compile(
                                                    "(?:^|&)nonce_str=([0-9a-f]{32})(?:&|$)")
                                            .matcher(form);
                            org.junit.jupiter.api.Assertions.assertTrue(nonce.find());
                            org.junit.jupiter.api.Assertions.assertTrue(nonces.add(nonce.group(1)));
                            return ProviderHttpResponse.buffered(
                                    calls.incrementAndGet() == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"code\":100,\"data\":{\"deductType\":1,\"balance\":42}}"
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
                assertEquals(42L, result.getSingleOutput(GetAccountBalanceOutput.class).balance);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
