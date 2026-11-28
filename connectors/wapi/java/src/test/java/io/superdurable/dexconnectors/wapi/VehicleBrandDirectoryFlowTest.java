// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.wapi;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.wapi.VehicleBrandDirectory.ListVehicleBrandsOperation;

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
class VehicleBrandDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListVehicleBrandsOperation operation =
                new ListVehicleBrandsOperation(
                        call ->
                                new VehicleBrandDirectory.Credentials(
                                        "123",
                                        new SecretString("REDACTEDAPPSECRET000000000000000")),
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://uaqy.api.storeapi.net/pyi/170/324?appid=123&format=json&sign=cdf4f0a9fb8d9e9000b8f4e014b9e41b&time=1795089600",
                                    request.getUrl());
                            String brand = "{\"ppid\":\"15\",\"pp_name\":\"宝马\",\"pp_price\":3400}";
                            String body =
                                    "{\"codeid\":\"10000\",\"retdata\":["
                                            + String.join(",", Collections.nCopies(201, brand))
                                            + "]}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20,
                        java.time.Clock.fixed(
                                java.time.Instant.ofEpochSecond(1795089600L),
                                java.time.ZoneOffset.UTC));
        VehicleBrandDirectoryWorker.VehicleBrandDirectoryFlow flow =
                new VehicleBrandDirectoryWorker.VehicleBrandDirectoryFlow(operation);
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
                new Thread(serving, "vehicle-brand-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-vehicle-brand-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new VehicleBrandDirectory.ListVehicleBrandsInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ListVehicleBrandsOutput page =
                        result.getSingleOutput(ListVehicleBrandsOutput.class);
                assertEquals("15", page.brands.get(0).id);
                assertEquals("宝马", page.brands.get(0).name);
                assertEquals(3400, page.brands.get(0).priceFen);
                assertEquals(201, page.total);
                assertEquals(200, page.brands.size());
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
