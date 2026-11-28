// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.newayz;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.newayz.CurrentWeather.GetCurrentWeatherInput;
import io.superdurable.dexconnectors.newayz.CurrentWeather.GetCurrentWeatherOperation;
import io.superdurable.dexconnectors.newayz.CurrentWeather.GetCurrentWeatherOutput;
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
class CurrentWeatherFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        GetCurrentWeatherOperation operation =
                new GetCurrentWeatherOperation(
                        call -> new SecretString("test-key"),
                        request ->
                                ProviderHttpResponse.buffered(
                                        calls.incrementAndGet() == 1 ? 503 : 200,
                                        ProviderHttpHeaders.EMPTY,
                                        "{\"status\":\"1\",\"infocode\":\"10000\",\"lives\":[{\"city\":\"Wuhan\",\"adcode\":\"420100\",\"temperature\":\"20\",\"weather\":\"Clear\"}]}"
                                                .getBytes(StandardCharsets.UTF_8)),
                        1 << 20);
        CurrentWeatherWorker.WeatherFlow flow = new CurrentWeatherWorker.WeatherFlow(operation);
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
                new Thread(serving, "current-weather-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-current-weather-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new GetCurrentWeatherInput("420100"));
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                assertEquals("Wuhan", result.getSingleOutput(GetCurrentWeatherOutput.class).city);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
