// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.worktile;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.worktile.ProjectDirectory.ListProjectsOperation;

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
class ProjectDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListProjectsOperation operation =
                new ListProjectsOperation(
                        call ->
                                new ProjectDirectory.Credentials(
                                        new SecretString("client-id"),
                                        new SecretString("test-secret")),
                        request -> {
                            int index = calls.getAndIncrement();
                            boolean isAuth = index % 2 == 0;
                            assertEquals(
                                    isAuth
                                            ? "/open-api/tenant-access-token"
                                            : "/open-api/mission/get-projects-by-page",
                                    java.net.URI.create(request.getUrl()).getPath());
                            String body =
                                    isAuth
                                            ? "{\"tenant_access_token\":\"REDACTED_WORKTILE_ACCESS_TOKEN\",\"expires_in\":7200}"
                                            : "{\"page_index\":1,\"page_size\":10,\"has_more\":true,\"items\":[{\"_id\":\"project-1\",\"name\":\"Example"
                                                  + " Project\",\"identifier\":\"ABC\",\"visibility\":1}]}";
                            return ProviderHttpResponse.buffered(
                                    index == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        ProjectDirectoryWorker.ProjectDirectoryFlow flow =
                new ProjectDirectoryWorker.ProjectDirectoryFlow(operation);
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
                new Thread(serving, "project-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-project-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new ProjectDirectory.ListProjectsInput(1, 10));
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListProjectsOutput page = result.getSingleOutput(ListProjectsOutput.class);
                assertEquals("project-1", page.projects.get(0).id);
                assertEquals(true, page.hasMore);
                assertEquals(4, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
