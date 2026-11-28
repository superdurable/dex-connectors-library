// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.SecretString;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import java.nio.file.Files;
import java.util.Arrays;
import java.util.Objects;

/** Serve the single-worker and directory Flows on the workday connection. */
public final class WorkerDirectoryWorker {
    private WorkerDirectoryWorker() {}

    /**
     * Start a Worker using a tenant access token supplied by its environment. Hosted applications
     * use WorkdayClient.fromProject for renewal.
     */
    public static void main(String[] args) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        String baseUrl =
                Objects.requireNonNull(
                        System.getenv("WORKDAY_BASE_URL"), "WORKDAY_BASE_URL is required");
        String tokenEndpoint =
                Objects.requireNonNull(
                        System.getenv("WORKDAY_TOKEN_ENDPOINT"),
                        "WORKDAY_TOKEN_ENDPOINT is required");
        String token =
                Objects.requireNonNull(
                        System.getenv("WORKDAY_ACCESS_TOKEN"), "WORKDAY_ACCESS_TOKEN is required");
        WorkdayCredentials credentials =
                new WorkdayCredentials(
                        "", new SecretString(""), new SecretString(token), new SecretString(""));
        WorkdayClient client =
                new WorkdayClient(
                        call -> credentials,
                        baseUrl,
                        tokenEndpoint,
                        new ProviderHttpClient(
                                java.time.Duration.ofSeconds(15), AddressPolicy.PUBLIC_HTTPS),
                        1 << 20);
        Registry registry =
                new Registry(
                        Arrays.<Flow<?>>asList(
                                new GetWorkerFlow(client.getWorker()),
                                new ListWorkersFlow(client.listWorkers())));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        Files.createTempDirectory("workday-blobs").toString(),
                                        16L << 20));
                Worker worker =
                        new Worker(
                                registry,
                                cache,
                                WorkerOptions.newBuilder().serverAddress(address).build())) {
            worker.start();
        }
    }
}
