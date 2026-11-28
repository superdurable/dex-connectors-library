// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.zentao;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;
import io.superdurable.dexconnectors.zentao.ProductDirectory.ListProductsOperation;

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
class ProductDirectoryFlowTest {
    @Test
    void retryThenComplete(@TempDir Path directory) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        AtomicInteger calls = new AtomicInteger();
        ListProductsOperation operation =
                new ListProductsOperation(
                        call -> new SecretString("test-key"),
                        "https://zentao.example.org/zentao",
                        request -> {
                            int index = calls.getAndIncrement();
                            assertEquals(
                                    "https://zentao.example.org/zentao/api.php/v1/products",
                                    request.getUrl());
                            String body =
                                    "{\"total\":1,\"products\":[{\"id\":6,\"name\":\"Example\",\"code\":\"example\",\"type\":\"normal\",\"acl\":\"private\"}]}";
                            return ProviderHttpResponse.buffered(
                                    index == 0 ? 503 : 200,
                                    ProviderHttpHeaders.EMPTY,
                                    body.getBytes(StandardCharsets.UTF_8));
                        },
                        1 << 20);
        ProductDirectoryWorker.ProductDirectoryFlow flow =
                new ProductDirectoryWorker.ProductDirectoryFlow(operation);
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
                new Thread(serving, "product-directory-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "java-product-directory-" + UUID.randomUUID();
                client.startFlow(flow, flowId, new ProductDirectory.ListProductsInput());
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                ProductList page = result.getSingleOutput(ProductList.class);
                assertEquals("private", page.products.get(0).accessControl);
                assertEquals("example", page.products.get(0).code);
                assertEquals(6, page.products.get(0).id);
                assertEquals("Example", page.products.get(0).name);
                assertEquals("normal", page.products.get(0).type);
                assertEquals(1, page.total);
                assertEquals(2, calls.get());
            }
            serving.get(30, TimeUnit.SECONDS);
        }
    }
}
