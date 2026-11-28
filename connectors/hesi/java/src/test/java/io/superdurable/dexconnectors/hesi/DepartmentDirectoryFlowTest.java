// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.hesi;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.hesi.DepartmentDirectory.ListDepartmentsOperation;
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
class DepartmentDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListDepartmentsOperation operation =
                new ListDepartmentsOperation(
                        call ->
                                new DepartmentDirectory.Credentials(
                                        new SecretString("key"), new SecretString("secret")),
                        "https://app.ekuaibao.com",
                        request -> {
                            int index = calls.getAndIncrement();
                            boolean isAuth = index % 2 == 0;
                            assertEquals(
                                    isAuth
                                            ? "/api/openapi/v1/auth/getAccessToken"
                                            : "/api/openapi/v1/departments",
                                    java.net.URI.create(request.getUrl()).getPath());
                            return ProviderHttpResponse.buffered(
                                    index == 1 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    (isAuth
                                                    ? "{\"value\":{\"accessToken\":\"test-access\"}}"
                                                    : "{\"count\":2,\"items\":[{\"id\":\"D1\",\"name\":\"Domain\",\"active\":true}]}")
                                            .getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        DepartmentDirectoryWorker.DepartmentDirectoryFlow flow =
                new DepartmentDirectoryWorker.DepartmentDirectoryFlow(operation);
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
                new Thread(serving, "department-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-department-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, "");
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                assertEquals(
                        "D1",
                        result.getSingleOutput(ListDepartmentsOutput.class).departments.get(0).id);
                assertEquals(1L, result.getSingleOutput(ListDepartmentsOutput.class).nextStart);
                assertEquals(4, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
