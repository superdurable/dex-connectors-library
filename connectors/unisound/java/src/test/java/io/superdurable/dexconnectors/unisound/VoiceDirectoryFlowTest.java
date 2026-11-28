// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.unisound;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.unisound.VoiceDirectory.ListSystemVoicesOperation;

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
class VoiceDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListSystemVoicesOperation operation =
                new ListSystemVoicesOperation(
                        call -> new SecretString("test-key"),
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://maas-api.unisound.com/v1/audio/voices/query",
                                    request.getUrl());
                            String body =
                                    "{\"system_voice\":[{\"voice_id\":\"cn_male_chenyu\",\"voice_name\":\"晨宇\",\"language\":[\"zh\"]}],\"base_resp\":{\"status_code\":0}}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        VoiceDirectoryWorker.VoiceDirectoryFlow flow =
                new VoiceDirectoryWorker.VoiceDirectoryFlow(operation);
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
                new Thread(serving, "voice-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-voice-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new VoiceDirectory.ListSystemVoicesInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListSystemVoicesOutput page = result.getSingleOutput(ListSystemVoicesOutput.class);
                assertEquals(java.util.Arrays.asList("zh"), page.voices.get(0).languages);
                assertEquals("晨宇", page.voices.get(0).name);
                assertEquals("cn_male_chenyu", page.voices.get(0).voiceID);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
