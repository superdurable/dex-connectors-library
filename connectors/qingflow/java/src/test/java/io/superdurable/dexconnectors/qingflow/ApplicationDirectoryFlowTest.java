// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.qingflow;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.qingflow.ApplicationDirectory.ListApplicationsOperation;
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
class ApplicationDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListApplicationsOperation operation =
                new ListApplicationsOperation(
                        call -> new SecretString("test-key"),
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals("https://api.qingflow.com/app", request.getUrl());
                            String body =
                                    "{\"errCode\":0,\"result\":{\"appList\":[{\"appKey\":\"app_example\",\"appName\":\"Operations\"}]}}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        ApplicationDirectoryWorker.ApplicationDirectoryFlow flow =
                new ApplicationDirectoryWorker.ApplicationDirectoryFlow(operation);
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
                new Thread(serving, "application-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-application-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new ApplicationDirectory.ListApplicationsInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListApplicationsOutput page = result.getSingleOutput(ListApplicationsOutput.class);
                assertEquals("app_example", page.applications.get(0).appKey);
                assertEquals("Operations", page.applications.get(0).appName);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
