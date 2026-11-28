// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.istero;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.istero.AccountStatus.GetAccountStatusOperation;
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
class AccountStatusFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        GetAccountStatusOperation operation =
                new GetAccountStatusOperation(
                        call ->
                                new AccountStatus.Credentials(
                                        "60066023", new SecretString("private-test-token")),
                        request -> {
                            assertEquals(
                                    "https://api.istero.com/resource/v1/isas/mine/information?isid=60066023",
                                    request.getUrl());
                            return ProviderHttpResponse.buffered(
                                    calls.incrementAndGet() == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    "{\"code\":200,\"data\":{\"isid\":60066023,\"today_used\":3,\"today_remaining\":997}}"
                                            .getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        AccountStatusWorker.AccountStatusFlow flow =
                new AccountStatusWorker.AccountStatusFlow(operation);
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
                new Thread(serving, "account-status-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-account-status-" + UUID.randomUUID();
                client.startFlow(flow, flowId, "");
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                GetAccountStatusOutput output =
                        result.getSingleOutput(GetAccountStatusOutput.class);
                assertEquals(60066023, output.isid);
                assertEquals(3, output.todayUsed);
                assertEquals(997, output.todayRemaining);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
