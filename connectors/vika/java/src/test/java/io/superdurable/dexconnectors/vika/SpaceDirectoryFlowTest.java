// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.vika;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.vika.SpaceDirectory.ListSpacesOperation;

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
class SpaceDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListSpacesOperation operation =
                new ListSpacesOperation(
                        call -> new SecretString("test-key"),
                        "https://vika.cn",
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals("https://vika.cn/fusion/v1/spaces", request.getUrl());
                            String body =
                                    "{\"success\":true,\"code\":200,\"data\":{\"spaces\":[{\"id\":\"spczdmQDfBAn5\",\"name\":\"Example"
                                        + " Workspace\",\"isAdmin\":true}]}}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        SpaceDirectoryWorker.SpaceDirectoryFlow flow =
                new SpaceDirectoryWorker.SpaceDirectoryFlow(operation);
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
                new Thread(serving, "space-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-space-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new SpaceDirectory.ListSpacesInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                SpacePage page = result.getSingleOutput(SpacePage.class);
                assertEquals("spczdmQDfBAn5", page.spaces.get(0).id);
                assertEquals("Example Workspace", page.spaces.get(0).name);
                assertEquals(true, page.spaces.get(0).isAdmin);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
