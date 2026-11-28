// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rainbond;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.rainbond.TeamDirectory.ListTeamsOperation;
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
class TeamDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListTeamsOperation operation =
                new ListTeamsOperation(
                        call -> new SecretString("test-key"),
                        "https://rainbond.example.org",
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://rainbond.example.org/openapi/v1/teams?page=1&page_size=20",
                                    request.getUrl());
                            String body =
                                    "{\"total\":1,\"tenants\":[{\"tenant_id\":\"team-1\",\"tenant_name\":\"Engineering\",\"is_active\":true}]}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        TeamDirectoryWorker.TeamDirectoryFlow flow =
                new TeamDirectoryWorker.TeamDirectoryFlow(operation);
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
                new Thread(serving, "team-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-team-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new TeamDirectory.ListTeamsInput(1, 20));
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListTeamsOutput page = result.getSingleOutput(ListTeamsOutput.class);
                assertEquals("team-1", page.teams.get(0).id);
                assertEquals("Engineering", page.teams.get(0).name);
                assertEquals(true, page.teams.get(0).active);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
