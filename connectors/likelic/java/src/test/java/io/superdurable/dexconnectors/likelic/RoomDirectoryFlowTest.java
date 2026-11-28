// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.likelic;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.likelic.RoomDirectory.ListRoomsOperation;
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
class RoomDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListRoomsOperation operation =
                new ListRoomsOperation(
                        call ->
                                new RoomDirectory.Credentials(
                                        "demo_company",
                                        new SecretString(
                                                "sk-0123456789abcdef0123456789abcdef0123456789abcdef")),
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://agent.likelic.com/api/v1/rooms?page=1&limit=20",
                                    request.getUrl());
                            String body =
                                    "{\"success\":true,\"data\":[{\"channel\":\"wework\",\"instance_id\":3,\"room_id\":\"room-123\",\"push_state\":\"ready\",\"room_name\":\"private"
                                        + " group\",\"company_name\":\"private"
                                        + " customer\",\"member_count\":12}],\"meta\":{\"page\":1,\"limit\":20,\"total\":1}}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        RoomDirectoryWorker.RoomDirectoryFlow flow =
                new RoomDirectoryWorker.RoomDirectoryFlow(operation);
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
                new Thread(serving, "room-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-room-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new RoomDirectory.ListRoomsInput(1));
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListRoomsOutput page = result.getSingleOutput(ListRoomsOutput.class);
                assertEquals("wework", page.rooms.get(0).channel);
                assertEquals(3, page.rooms.get(0).instanceID);
                assertEquals("ready", page.rooms.get(0).pushState);
                assertEquals("room-123", page.rooms.get(0).roomID);
                assertEquals(1, page.page);
                assertEquals(1, page.total);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
