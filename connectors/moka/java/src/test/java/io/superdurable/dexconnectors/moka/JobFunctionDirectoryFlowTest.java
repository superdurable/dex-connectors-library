// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.moka;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.moka.JobFunctionDirectory.ListJobFunctionsOperation;
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
class JobFunctionDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListJobFunctionsOperation operation =
                new ListJobFunctionsOperation(
                        call -> new SecretString("test-key"),
                        "orgid-test",
                        "campus",
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/orgid-test?mode=campus",
                                    request.getUrl());
                            String body =
                                    "{\"jobs\":[{\"id\":2873,\"label\":\"Technology\",\"jobCount\":64,\"email\":\"must"
                                        + " not persist\"}]}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        JobFunctionDirectoryWorker.JobFunctionDirectoryFlow flow =
                new JobFunctionDirectoryWorker.JobFunctionDirectoryFlow(operation);
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
                new Thread(serving, "job-function-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-job-function-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new JobFunctionDirectory.ListJobFunctionsInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListJobFunctionsOutput page = result.getSingleOutput(ListJobFunctionsOutput.class);
                assertEquals(2873, page.functions.get(0).id);
                assertEquals(64, page.functions.get(0).jobCount);
                assertEquals("Technology", page.functions.get(0).label);
                assertEquals(0, page.functions.get(0).parentId);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
