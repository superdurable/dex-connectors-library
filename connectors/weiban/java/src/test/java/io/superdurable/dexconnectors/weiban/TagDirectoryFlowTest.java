// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.weiban;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.weiban.TagDirectory.ListTagsOperation;

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
class TagDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListTagsOperation operation =
                new ListTagsOperation(
                        call ->
                                new TagDirectory.Credentials(
                                        "corp_test", new SecretString("test-secret")),
                        "https://open.weibanzhushou.com",
                        request -> {
                            int index = calls.getAndIncrement();
                            boolean isAuth = index % 2 == 0;
                            assertEquals(
                                    isAuth ? "/open-api/access_token/get" : "/open-api/tag/list",
                                    java.net.URI.create(request.getUrl()).getPath());
                            String token = "fixture+token&" + (index / 2);
                            if (!isAuth)
                                assertEquals(
                                        "access_token=fixture%2Btoken%26"
                                                + (index / 2)
                                                + "&limit=10&offset=0",
                                        java.net.URI.create(request.getUrl()).getRawQuery());
                            String body =
                                    isAuth
                                            ? "{\"access_token\":\""
                                                    + token
                                                    + "\",\"errcode\":0,\"errmsg\":\"ok\",\"expires_in\":7200}"
                                            : "{\"errcode\":0,\"errmsg\":\"ok\",\"total\":1,\"tag_list\":[{\"tag_id\":\"tag_123\",\"tag_name\":\"Returning\",\"tag_group\":\"Lifecycle\"}]}";
                            return ProviderHttpResponse.buffered(
                                    index == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        TagDirectoryWorker.TagDirectoryFlow flow =
                new TagDirectoryWorker.TagDirectoryFlow(operation);
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
                new Thread(serving, "tag-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-tag-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new TagDirectory.ListTagsInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                TagsResult page = result.getSingleOutput(TagsResult.class);
                assertEquals("Lifecycle", page.tags.get(0).group);
                assertEquals("tag_123", page.tags.get(0).id);
                assertEquals("Returning", page.tags.get(0).name);
                assertEquals(1, page.total);
                assertEquals(4, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
