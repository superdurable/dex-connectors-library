// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.Credentials;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderTransportException;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.stream.Stream;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

class ProjectCredentialProviderTest {
    private static final ConnectionKey KEY = new ConnectionKey("sample", "main");
    private static final Call CALL = Call.newTriggerCall(new ConnectionRef("sample", "main"),
            new OperationRef("sample", "createItem"), CallId.of("5af8fafa-06ff-5b2d-bb13-04693809fabf"));
    private static final Scope SCOPE = new Scope("test-project", "preview");

    private Path temporaryDirectory;
    private final ExecutorService executor = Executors.newCachedThreadPool();

    @BeforeEach
    void createTemporaryDirectory() throws IOException {
        temporaryDirectory = Files.createTempDirectory(Paths.get("/tmp"), "sdkjava-provider-");
    }

    @AfterEach
    void cleanUp() throws IOException {
        executor.shutdownNow();
        try (Stream<Path> paths = Files.walk(temporaryDirectory)) {
            paths.sorted(Comparator.reverseOrder()).forEach(path -> path.toFile().delete());
        }
    }

    @Test
    void admitsOneRefreshAcrossReplicas() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final ConnectionStore firstStore = new ConnectionStore(objects, SCOPE);
        final ConnectionStore secondStore = new ConnectionStore(objects, SCOPE);
        firstStore.replaceCredential(KEY, 0, material("old", Instant.now().minusSeconds(60)));
        final RefreshingProjectCredentialProvider<ObjectNode> first = provider(firstStore);
        final RefreshingProjectCredentialProvider<ObjectNode> second = provider(secondStore);
        final RecordingDriver driver = new RecordingDriver();
        final Future<ObjectNode> firstResult = executor.submit(() -> Credentials.resolveCredential(first, CALL, driver));
        assertTrue(driver.started.await(2, TimeUnit.SECONDS));
        final Future<ObjectNode> secondResult = executor.submit(() -> Credentials.resolveCredential(second, CALL, driver));
        driver.release.countDown();
        assertEquals(credential("new"), firstResult.get(3, TimeUnit.SECONDS));
        assertEquals(credential("new"), secondResult.get(3, TimeUnit.SECONDS));
        assertEquals(1, driver.calls.get());
        assertEquals("READY", firstStore.readConnection(KEY).getStatus());
        assertEquals("kept", firstStore.readCredentialMaterial(KEY).getMaterial().getCredentials().get("refresh").textValue());
    }

    @Test
    void recoversAbandonedAuthorizationOrFencesReauthorization() throws Exception {
        final ConnectionStore pendingStore = new ConnectionStore(new InMemoryObjectStore(), SCOPE);
        authorizeRecord(pendingStore, Instant.now().plusSeconds(60));
        assertThrows(ExchangePendingException.class,
                () -> Credentials.resolveCredential(provider(pendingStore), CALL, new RecordingDriver()));

        final ConnectionStore expiredStore = new ConnectionStore(new InMemoryObjectStore(), SCOPE);
        authorizeRecord(expiredStore, Instant.now().minusSeconds(60));
        assertThrows(ReauthorizationRequiredException.class,
                () -> Credentials.resolveCredential(provider(expiredStore), CALL, new RecordingDriver()));
        assertEquals("REAUTHORIZATION_REQUIRED", expiredStore.readConnection(KEY).getStatus());

        final ConnectionStore completedStore = new ConnectionStore(new InMemoryObjectStore(), SCOPE);
        final String resultKey = authorizeRecord(completedStore, Instant.now().minusSeconds(60));
        final ObjectNode authorized = GoCompatibleJson.newObject().put("access", "authorized").put("refresh", "kept");
        completedStore.getObjects().createObject(resultKey, GoCompatibleJson.encode(ConnectionDocuments.credentialEnvelope(
                SCOPE, KEY, 1, authorized, Rfc3339NanoTimestamp.fromInstant(Instant.now().plusSeconds(3600)), "oauth2",
                "authorization-1")));
        assertEquals("authorized", Credentials.resolveCredential(provider(completedStore), CALL, new RecordingDriver())
                .get("access").textValue());
        assertEquals("READY", completedStore.readConnection(KEY).getStatus());
    }

    @Test
    void observersTreatAnExpiredAdmissionWithoutAResultAsUnknown() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final ConnectionStore firstStore = new ConnectionStore(objects, SCOPE);
        firstStore.replaceCredential(KEY, 0, material("current", Instant.now().plusSeconds(3600)));
        final RefreshingProjectCredentialProvider<ObjectNode> first = provider(firstStore);
        first.admitRefresh(firstStore.readRecord(KEY).record, Instant.now().minusSeconds(120));
        final RefreshingProjectCredentialProvider<ObjectNode> second = provider(new ConnectionStore(objects, SCOPE));
        final RecordingDriver driver = new RecordingDriver();
        // The abandoned refresh may have rotated the token, so no replica restores READY or exchanges it.
        assertThrows(ReauthorizationRequiredException.class, () -> Credentials.resolveCredential(first, CALL, driver));
        assertThrows(ReauthorizationRequiredException.class, () -> Credentials.resolveCredential(second, CALL, driver));
        assertEquals("REAUTHORIZATION_REQUIRED", firstStore.readConnection(KEY).getStatus());
        assertEquals(0, driver.calls.get());
    }

    @Test
    void keepsPriorMaterialOnRetryableFailureAndFencesTerminalFailure() throws Exception {
        final ConnectionStore store = new ConnectionStore(new InMemoryObjectStore(), SCOPE);
        store.replaceCredential(KEY, 0, material("old", Instant.now().minusSeconds(60)));
        final RefreshingProjectCredentialProvider<ObjectNode> provider = provider(store);
        final RecordingDriver retryable = new RecordingDriver();
        retryable.error = new ProviderTransportException(true, null, null);
        retryable.release.countDown();
        final RefreshFailedException failed = assertThrows(RefreshFailedException.class,
                () -> Credentials.resolveCredential(provider, CALL, retryable));
        assertFalse(String.valueOf(failed.getMessage()).contains("inert provider detail"));
        assertEquals("READY", store.readConnection(KEY).getStatus());

        final ConnectionStore ambiguousStore = new ConnectionStore(new InMemoryObjectStore(), SCOPE);
        ambiguousStore.replaceCredential(KEY, 0, material("old", Instant.now().minusSeconds(60)));
        final RecordingDriver ambiguous = new RecordingDriver();
        ambiguous.error = new IOException("inert provider detail");
        ambiguous.release.countDown();
        // A failure after the request may have left cannot prove the token was not rotated.
        assertThrows(ReauthorizationRequiredException.class,
                () -> Credentials.resolveCredential(provider(ambiguousStore), CALL, ambiguous));
        assertEquals("REAUTHORIZATION_REQUIRED", ambiguousStore.readConnection(KEY).getStatus());
        final RecordingDriver terminal = new RecordingDriver();
        terminal.error = new ReauthorizationRequiredException();
        terminal.release.countDown();
        assertThrows(ReauthorizationRequiredException.class, () -> Credentials.resolveCredential(provider, CALL, terminal));
        assertEquals("REAUTHORIZATION_REQUIRED", store.readConnection(KEY).getStatus());
        assertThrows(ReauthorizationRequiredException.class, () -> Credentials.resolveCredential(provider, CALL, terminal));
        assertEquals(1, terminal.calls.get());
    }

    @Test
    void rejectionRequiresAuthoritativeExpiry() throws Exception {
        final ConnectionStore store = new ConnectionStore(new InMemoryObjectStore(), SCOPE);
        store.replaceCredential(KEY, 0, material("valid", Instant.now().plusSeconds(3600)));
        final RecordingDriver driver = new RecordingDriver();
        assertThrows(RejectedCredentialNotRefreshedException.class,
                () -> Credentials.resolveCredentialAfterRejection(provider(store), CALL, driver));
        assertEquals(0, driver.calls.get());
    }

    @Test
    void routesSharedConnectionsThroughBrokerWithoutRefreshDriver() throws Exception {
        final List<JsonNode> requests = new ArrayList<>();
        final BrokerHttpTransport transport = (url, token, body, maximum) -> {
            requests.add(MAPPER.readTree(body));
            final ObjectNode answer = GoCompatibleJson.newObject();
            answer.put("status", "READY");
            answer.putObject("credential").put("access", "shared-" + requests.size());
            answer.put("revision", requests.size());
            answer.put("expiresAt", Rfc3339NanoTimestamp.fromInstant(Instant.now().plusSeconds(3600)).toWire());
            return new BrokerHttpTransport.Response(200, MAPPER.writeValueAsBytes(answer));
        };
        final LoadedProject project = loadSharedProject(transport);
        final ConnectionStore store = project.getConnections();
        store.replaceCredential(KEY, 0, new CredentialMaterial(credential("ordinary"), null, "oauth2"));
        assertEquals("ordinary", new ProjectCredentialProvider<>(store, KEY, ProjectCredentialProviderTest::decode)
                .resolve(CALL).get("access").textValue());
        final ConnectionKey sharedKey = new ConnectionKey("sample", "shared");
        final Call sharedCall = Call.newTriggerCall(new ConnectionRef("sample", "shared"), CALL.getOperation(), CALL.getId());
        final RefreshingProjectCredentialProvider<ObjectNode> refreshing = new RefreshingProjectCredentialProvider<>(
                store, sharedKey, ProjectCredentialProviderTest::decode, value -> value,
                ProjectCredentialProviderTest::decodeShared);
        final CredentialRefreshDriver<ObjectNode> forbidden = new CredentialRefreshDriver<ObjectNode>() {
            @Override
            public boolean isRefreshRequired(final State<ObjectNode> state) {
                throw new AssertionError("shared credential consulted refresh driver");
            }

            @Override
            public Result<ObjectNode> refresh(final State<ObjectNode> state) {
                throw new AssertionError("shared credential dispatched refresh");
            }
        };
        assertEquals("shared-1", Credentials.resolveCredential(refreshing, sharedCall, forbidden).get("access").textValue());
        assertEquals("shared-2",
                Credentials.resolveCredentialAfterRejection(refreshing, sharedCall, forbidden).get("access").textValue());
        assertEquals(MAPPER.readTree("[{\"sharedConnectionId\":\"shared-7\"},"
                + "{\"sharedConnectionId\":\"shared-7\",\"rejectedRevision\":1}]"), MAPPER.valueToTree(requests));
        assertEquals("shared-2", new ProjectCredentialProvider<>(store, sharedKey, ProjectCredentialProviderTest::decode,
                ProjectCredentialProviderTest::decodeShared).resolve(sharedCall).get("access").textValue());
        final IllegalArgumentException decodingError = assertThrows(IllegalArgumentException.class,
                () -> new ProjectCredentialProvider<>(store, sharedKey, ProjectCredentialProviderTest::decode)
                        .resolve(sharedCall));
        assertEquals("shared project credential decoding failed", decodingError.getMessage());
    }

    @Test
    void propagatesTerminalBrokerStatuses() throws Exception {
        for (String status : new String[] {"REAUTHORIZATION_REQUIRED", "CONNECTION_NOT_SELECTED"}) {
            final LoadedProject project = loadSharedProject((url, token, body, maximum) ->
                    new BrokerHttpTransport.Response(200,
                            ("{\"status\":\"" + status + "\"}").getBytes(StandardCharsets.UTF_8)));
            final Call sharedCall =
                    Call.newTriggerCall(new ConnectionRef("sample", "shared"), CALL.getOperation(), CALL.getId());
            final RefreshingProjectCredentialProvider<ObjectNode> provider = new RefreshingProjectCredentialProvider<>(
                    project.getConnections(), new ConnectionKey("sample", "shared"),
                    ProjectCredentialProviderTest::decode, value -> value, ProjectCredentialProviderTest::decodeShared);
            final Class<? extends RuntimeException> expected = "REAUTHORIZATION_REQUIRED".equals(status)
                    ? ReauthorizationRequiredException.class : ConnectionNotSelectedException.class;
            assertThrows(expected, () -> Credentials.resolveCredential(provider, sharedCall, new RecordingDriver()));
        }
    }

    private static LoadedProject loadSharedProject(final BrokerHttpTransport transport) throws IOException {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final ConfigurationStore configurations = new ConfigurationStore(objects, SCOPE);
        final ApplicationSecretRef tokenReference = configurations.createCredentialBrokerToken("scope-token");
        final Configuration configuration = Configuration.newBuilder(SCOPE, 1)
                .addConnection(new ConnectionConfiguration("sample", "main", "example", "sample",
                        GoCompatibleJson.newObject()))
                .addConnection(new ConnectionConfiguration("sample", "shared", "example", "sample",
                        GoCompatibleJson.newObject(), "", "shared-7"))
                .credentialBroker(new CredentialBrokerConfiguration("https://broker.example/read", tokenReference))
                .build();
        final Configuration written = configurations.writeConfiguration(0, configuration).getConfiguration();
        final SnapshotRef snapshot = configurations.freezeConfiguration(written.getRevision());
        return ProjectConfigurationLoader.loadProjectFromObjectStore(objects, SCOPE, snapshot, transport);
    }

    private static String authorizeRecord(final ConnectionStore store, final Instant deadline) throws IOException {
        final String path = KEY.path(store.getScope());
        final String resultKey = path + "/exchange-results/authorization-1";
        final ObjectNode mutation = GoCompatibleJson.newObject();
        mutation.put("kind", "AUTHORIZING").put("invocationId", "invocation-1").put("id", "authorization-1")
                .put("fence", 1).put("baseRevision", 0)
                .put("deadline", Rfc3339NanoTimestamp.fromInstant(deadline).toWire()).put("resultKey", resultKey);
        store.getObjects().createObject(path + "/head", GoCompatibleJson.encode(ConnectionDocuments.connectionRecord(
                store.getScope(), KEY, 1, 1, "AUTHORIZING", null, "oauth2", null, mutation)));
        return resultKey;
    }

    private static RefreshingProjectCredentialProvider<ObjectNode> provider(final ConnectionStore store) {
        return new RefreshingProjectCredentialProvider<>(store, KEY, ProjectCredentialProviderTest::decode,
                ObjectNode::deepCopy);
    }

    private static ObjectNode decode(final ObjectNode value) {
        if (value.get("access") == null || !value.get("access").isTextual()
                || value.get("refresh") == null || !value.get("refresh").isTextual()) {
            throw new IllegalArgumentException("credential is incomplete");
        }
        return credentialPair(value.get("access").textValue(), value.get("refresh").textValue());
    }

    private static ObjectNode decodeShared(final ObjectNode value) {
        if (value.get("access") == null || !value.get("access").isTextual()) {
            throw new IllegalArgumentException("shared credential is incomplete");
        }
        return GoCompatibleJson.newObject().put("access", value.get("access").textValue());
    }

    private static CredentialMaterial material(final String access, final Instant expiresAt) {
        return new CredentialMaterial(credential(access), Rfc3339NanoTimestamp.fromInstant(expiresAt), "oauth2");
    }

    private static ObjectNode credential(final String access) {
        return credentialPair(access, "kept");
    }

    private static ObjectNode credentialPair(final String access, final String refresh) {
        return GoCompatibleJson.newObject().put("access", access).put("refresh", refresh);
    }

    /** Observes one admitted provider exchange and holds it until released. */
    private static final class RecordingDriver implements CredentialRefreshDriver<ObjectNode> {
        private final AtomicInteger calls = new AtomicInteger();
        private final CountDownLatch started = new CountDownLatch(1);
        private final CountDownLatch release = new CountDownLatch(1);
        private volatile Exception error;

        @Override
        public boolean isRefreshRequired(final State<ObjectNode> state) {
            return state.getExpiresAt() != null && !state.getExpiresAt().isAfter(state.getNow());
        }

        @Override
        public Result<ObjectNode> refresh(final State<ObjectNode> state) throws IOException, InterruptedException {
            calls.incrementAndGet();
            started.countDown();
            if (!release.await(5, TimeUnit.SECONDS)) {
                throw new IOException("refresh was not released");
            }
            if (error instanceof IOException) {
                throw (IOException) error;
            }
            if (error instanceof RuntimeException) {
                throw (RuntimeException) error;
            }
            return new Result<>(credentialPair("new", state.getCredentials().get("refresh").textValue()),
                    Instant.now().plus(Duration.ofHours(1)));
        }
    }
}
