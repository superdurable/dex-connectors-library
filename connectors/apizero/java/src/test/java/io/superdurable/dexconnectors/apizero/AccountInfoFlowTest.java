// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.apizero;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.apizero.AccountInfo.GetAccountInfoOperation;
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
class AccountInfoFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        GetAccountInfoOperation operation =
                new GetAccountInfoOperation(
                        call -> new SecretString("sk_live_test-secret"),
                        request -> {
                            assertEquals("https://v1.apizero.cn/api/user-info", request.getUrl());
                            if (calls.incrementAndGet() == 1)
                                return ProviderHttpResponse.buffered(
                                        503, ProviderHttpHeaders.EMPTY, new byte[0]);
                            return ProviderHttpResponse.buffered(
                                    200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"code\":0,\"data\":{\"scope\":\"account\",\"balance\":{\"yuan\":\"123.4000\"},\"key\":{\"quota_remaining\":99}}}"
                                            .getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        AccountInfoWorker.AccountInfoFlow flow = new AccountInfoWorker.AccountInfoFlow(operation);
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
                new Thread(serving, "account-info-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-account-info-" + UUID.randomUUID();
                client.startFlow(flow, flowId, "");
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                GetAccountInfoOutput output = result.getSingleOutput(GetAccountInfoOutput.class);
                assertEquals("account", output.scope);
                assertEquals("123.4000", output.balanceYuan);
                assertEquals(null, output.keyQuotaRemaining);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
