// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.personio;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.*;

import io.superdurable.dex.*;
import io.superdurable.dexconnectors.sdk.projectconfig.*;
import io.superdurable.dexconnectors.sdk.providerhttp.*;

import org.junit.jupiter.api.*;
import org.junit.jupiter.api.io.TempDir;

import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

@Tag("dex-dev")
class PersonDirectoryFlowTest {

    @TempDir Path directory;

    @Test
    void refreshAndRetryLookup() throws Exception {
        Fixture fixture = new Fixture(true);
        PersonProfile result =
                read(
                        new GetPersonFlow(fixture.client.getPerson()),
                        new GetPersonInput("123"),
                        PersonProfile.class);
        assertEquals("123", result.id);
        fixture.verify();
    }

    @Test
    void refreshAndRetryDirectory() throws Exception {
        Fixture fixture = new Fixture(false);
        PersonPage page =
                read(
                        new ListPersonsFlow(fixture.client.listPersons()),
                        new ListPersonsInput(0, "", "", "", ""),
                        PersonPage.class);
        assertEquals("123", page.persons.get(0).id);
        assertEquals("page2", page.nextCursor);
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
                new Thread(serving, "personio-worker").start();
                worker.waitPullRegistered(Duration.ofSeconds(30));
                String flowId = "personio-" + UUID.randomUUID();
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
        final PersonioClient client;
        final LoadedProject project;

        Fixture(boolean lookup) throws Exception {
            project = PersonDirectoryWorker.exampleProject("test-id", "test-secret");
            PersonioClient.fromProject(project, "personio");
            client =
                    new PersonioClient(
                            PersonioCredentials.fromProject(project, "personio"),
                            "https://api.personio.de/v2",
                            request -> {
                                int status = 200;
                                String body;
                                if (request.getUrl().endsWith("/auth/token")) {
                                    assertEquals("POST", request.getMethod());
                                    assertEquals(
                                            "https://api.personio.de/v2/auth/token",
                                            request.getUrl());
                                    assertEquals(Duration.ofSeconds(8), request.getTimeout());
                                    Map<String, String> form = new HashMap<>();
                                    for (String pair :
                                            new String(request.getBody(), StandardCharsets.UTF_8)
                                                    .split("&")) {
                                        String[] parts = pair.split("=", 2);
                                        form.put(
                                                URLDecoder.decode(parts[0], "UTF-8"),
                                                URLDecoder.decode(parts[1], "UTF-8"));
                                    }
                                    assertEquals("test-id", form.get("client_id"));
                                    assertEquals("test-secret", form.get("client_secret"));
                                    assertEquals("client_credentials", form.get("grant_type"));
                                    assertEquals("personio:persons:read", form.get("scope"));
                                    exchanges.incrementAndGet();
                                    body =
                                            "{\"access_token\":\"test-bearer\",\"token_type\":\"Bearer\",\"scope\":\"personio:persons:read\",\"expires_in\":3600}";
                                } else {
                                    assertEquals("GET", request.getMethod());
                                    assertEquals(
                                            "Bearer test-bearer",
                                            request.getHeaders().get("Authorization"));
                                    assertEquals(
                                            "https://api.personio.de/v2/persons"
                                                    + (lookup ? "/123" : ""),
                                            request.getUrl());
                                    if (reads.incrementAndGet() == 1) status = 503;
                                    String profile =
                                            "{\"id\":\"123\",\"first_name\":\"Test\",\"email\":\"excluded@example.org\"}";
                                    body =
                                            lookup
                                                    ? profile
                                                    : "{\"_data\":["
                                                            + profile
                                                            + "],\"_meta\":{\"links\":{\"next\":{\"href\":\"/v2/persons?cursor=page2\"}}}}";
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
                            .readCredentialMaterial(new ConnectionKey("personio", "personio"))
                            .getMaterial();
            assertNotNull(stored.getExpiresAt());
            assertEquals("test-bearer", stored.getCredentials().path("access_token").asText());
            assertEquals("test-secret", stored.getCredentials().path("client_secret").asText());
        }
    }
}
