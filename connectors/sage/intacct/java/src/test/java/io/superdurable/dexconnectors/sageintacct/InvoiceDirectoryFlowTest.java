// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sageintacct;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.*;
import org.junit.jupiter.api.io.TempDir;

import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

@Tag("dex-dev")
class InvoiceDirectoryFlowTest {

    @TempDir Path directory;

    @Test
    void refreshAndRetryLookup() throws Exception {
        Fixture fixture = new Fixture(true);
        InvoiceSummary result =
                read(
                        new GetInvoiceFlow(fixture.client.getInvoice()),
                        new GetInvoiceInput("0123456789abcdef0123456789abcdef"),
                        InvoiceSummary.class);
        assertEquals("0123456789abcdef0123456789abcdef", result.key);
        fixture.verify();
    }

    @Test
    void refreshAndRetryDirectory() throws Exception {
        Fixture fixture = new Fixture(false);
        InvoiceList page =
                read(
                        new ListInvoicesFlow(fixture.client.listInvoices()),
                        new ListInvoicesInput(),
                        InvoiceList.class);
        assertEquals("0123456789abcdef0123456789abcdef", page.invoices.get(0).key);
        assertEquals("/page/2", page.next);
        fixture.verify();
    }

    private <I, O> O read(Flow<I> flow, I input, Class<O> output) throws Exception {
        String address = System.getenv().getOrDefault("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801");
        Registry registry = new Registry(Collections.<Flow<?>>singletonList(flow));
        try (BlobCache cache =
                        BlobCache.open(
                                new BlobCacheConfig(
                                        directory.resolve("blobs").toString(), 16L << 20));
                Client client = new Client(registry, cache, new ClientOptions(address))) {
            FutureTask<Void> serving;
            O value;
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
                new Thread(serving, "sage-intacct-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "sage-intacct-" + UUID.randomUUID();
                client.startFlow(flow, flowId, input);
                FlowResult result = client.waitForFlow(flowId, Duration.ofSeconds(30));
                assertEquals(FlowStatus.COMPLETED, result.getStatus(), result.getErrorMessage());
                value = result.getSingleOutput(output);
            }
            serving.get(30, TimeUnit.SECONDS);
            return value;
        }
    }

    static final class Fixture {
        final AtomicInteger exchanges = new AtomicInteger(), reads = new AtomicInteger();
        final SageIntacctClient client;
        final LoadedProject project;

        Fixture(boolean lookup) throws Exception {
            project = SageIntacctTestProject.exampleProject("test-id", "test-secret");
            SageIntacctClient.fromProject(project, "sage-intacct");
            client =
                    new SageIntacctClient(
                            SageIntacctCredentials.fromProject(project, "sage-intacct"),
                            "https://tenant.example.com",
                            request -> {
                                int status = 200;
                                String body;
                                if (request.getUrl().endsWith("/token")) {
                                    assertEquals("POST", request.getMethod());
                                    assertEquals(
                                            "https://api.intacct.com/ia/api/v1/oauth2/token",
                                            request.getUrl());
                                    String form =
                                            new String(request.getBody(), StandardCharsets.UTF_8);
                                    assertTrue(form.contains("client_id=test-id"));
                                    assertTrue(form.contains("client_secret=test-secret"));
                                    assertTrue(form.contains("grant_type=refresh_token"));
                                    assertTrue(form.contains("refresh_token=test-refresh"));
                                    exchanges.incrementAndGet();
                                    body =
                                            "{\"access_token\":\"test-bearer\",\"refresh_token\":\"rotated\",\"expires_in\":3600,\"token_type\":\"Bearer\"}";
                                } else {
                                    assertEquals("GET", request.getMethod());
                                    assertEquals(
                                            "Bearer test-bearer",
                                            request.getHeaders().get("Authorization"));
                                    assertEquals(
                                            "https://tenant.example.com/ia/api/v1/objects/accounts-receivable/invoice"
                                                    + (lookup
                                                            ? "/0123456789abcdef0123456789abcdef"
                                                            : ""),
                                            request.getUrl());
                                    if (reads.incrementAndGet() == 1) status = 503;
                                    String invoice =
                                            "{\"key\":\"0123456789abcdef0123456789abcdef\",\"id\":\"INV-42\",\"privateField\":42}";
                                    body =
                                            lookup
                                                    ? "{\"ia::result\":" + invoice + "}"
                                                    : "{\"ia::result\":["
                                                            + invoice
                                                            + "],\"ia::meta\":{\"next\":\"/page/2\"}}";
                                }
                                return ProviderHttpResponse.buffered(
                                        status,
                                        ProviderHttpHeaders.EMPTY,
                                        body.getBytes(StandardCharsets.UTF_8));
                            },
                            1 << 20);
        }

        void verify() throws Exception {
            assertEquals(1, exchanges.get());
            assertEquals(2, reads.get());
            CredentialMaterial stored =
                    project.getConnections()
                            .readCredentialMaterial(
                                    new ConnectionKey("sage-intacct", "sage-intacct"))
                            .getMaterial();
            assertNotNull(stored.getExpiresAt());
            assertEquals("rotated", stored.getCredentials().path("refresh_token").asText());
            assertEquals("test-bearer", stored.getCredentials().path("access_token").asText());
            assertEquals(
                    "test-secret", stored.getCredentials().path("oauth_client_secret").asText());
        }
    }
}
