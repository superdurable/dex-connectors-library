// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.Credentials;
import io.superdurable.dexconnectors.sdk.Failure;
import io.superdurable.dexconnectors.sdk.FailureKind;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.ReauthorizationRequiredException;
import io.superdurable.dexconnectors.sdk.RetryException;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import io.superdurable.dexconnectors.sdk.providerhttp.ProviderTransportException;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.attribute.PosixFilePermissions;
import java.time.Duration;
import java.time.Instant;
import java.util.Arrays;
import java.util.Base64;
import java.util.Comparator;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;
import java.util.stream.Stream;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

/** Dex Web local storage under concurrency; elapsed time is part of some behaviors under test. */
class DexWebStoreConcurrencyTest {
    private static final ConnectionKey KEY = new ConnectionKey("sample", "main");
    private static final Call CALL = Call.newTriggerCall(new ConnectionRef("sample", "main"),
            new OperationRef("sample", "createItem"), CallId.of("5af8fafa-06ff-5b2d-bb13-04693809fabf"));

    private Path temporaryDirectory;
    private Path connectionsPath;
    private final ExecutorService executor = Executors.newCachedThreadPool();

    @BeforeEach
    void createTemporaryDirectory() throws IOException {
        temporaryDirectory = Files.createTempDirectory(Paths.get("/tmp"), "sdkjava-dexweb-concurrency-");
        connectionsPath = temporaryDirectory.resolve("connections.json");
    }

    @AfterEach
    void cleanUp() throws IOException {
        executor.shutdownNow();
        try (Stream<Path> paths = Files.walk(temporaryDirectory)) {
            paths.sorted(Comparator.reverseOrder()).forEach(path -> path.toFile().delete());
        }
    }

    @Test
    void waiterOnTheSameStoreReturnsTheRefreshedCredentialAfterAdmission() throws Exception {
        writeConnections(credentials("old", "rotating-1"), Instant.now().minusSeconds(60));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final RefreshingProjectCredentialProvider<ObjectNode> provider = provider(project);
        final CountDownLatch admitted = new CountDownLatch(1);
        final CountDownLatch release = new CountDownLatch(1);
        final AtomicInteger calls = new AtomicInteger();
        final CredentialRefreshDriver<ObjectNode> driver = new ExpiryOnlyDriver() {
            @Override
            public Result<ObjectNode> refresh(final State<ObjectNode> state) throws InterruptedException {
                calls.incrementAndGet();
                admitted.countDown();
                release.await(10, TimeUnit.SECONDS);
                return new Result<>(credentials("new-1", "rotating-2"), Instant.now().plusSeconds(3600));
            }
        };
        final Future<ObjectNode> first = executor.submit(() -> Credentials.resolveCredential(provider, CALL, driver));
        assertTrue(admitted.await(10, TimeUnit.SECONDS));
        final Future<ObjectNode> second = executor.submit(() -> Credentials.resolveCredential(provider, CALL, driver));
        // The second caller must reach the REFRESHING head and wait for the lock before the first finishes.
        Thread.sleep(300);
        release.countDown();
        assertEquals("new-1", first.get(30, TimeUnit.SECONDS).get("access").textValue());
        assertEquals("new-1", second.get(30, TimeUnit.SECONDS).get("access").textValue());
        assertEquals(1, calls.get());
    }

    @Test
    void aliasesShareOneLockAndReentryIsRejected() throws Exception {
        writeConnections(credentials("old", "kept"), Instant.now().plusSeconds(3600));
        final Path link = Files.createSymbolicLink(temporaryDirectory.resolve("alias"), temporaryDirectory);
        final DexWebRefreshLock direct = new DexWebRefreshLock(connectionsPath, Duration.ofMillis(300));
        final DexWebRefreshLock throughLink = new DexWebRefreshLock(link.resolve("connections.json"),
                Duration.ofMillis(300));
        final DexWebRefreshLock throughParent = new DexWebRefreshLock(
                temporaryDirectory.resolve("alias").resolve("..").resolve(temporaryDirectory.getFileName())
                        .resolve("connections.json"), Duration.ofMillis(300));
        assertEquals(direct.getLockPath(), throughLink.getLockPath());
        assertEquals(direct.getLockPath(), throughParent.getLockPath());
        final DexWebRefreshLock.Held held = direct.acquire();
        try {
            assertThrows(IllegalStateException.class, throughLink::acquire);
            final Future<?> other = executor.submit(() -> {
                assertThrows(RefreshFailedException.class, throughLink::acquire);
                return null;
            });
            other.get(10, TimeUnit.SECONDS);
        } finally {
            held.close();
        }
        throughParent.acquire().close();
    }

    @Test
    void driverFinishingInTheGraceWindowIsPersistedUnderTheLock() throws Exception {
        writeConnections(credentials("old", "rotating-1"), Instant.now().minusSeconds(60));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final RefreshingProjectCredentialProvider<ObjectNode> provider = shortTimeouts(project,
                Duration.ofMillis(200), Duration.ofSeconds(5));
        final AtomicBoolean hasDriverExited = new AtomicBoolean();
        final AtomicInteger calls = new AtomicInteger();
        final CredentialRefreshDriver<ObjectNode> driver =
                lateDriver(Duration.ofSeconds(1), calls, hasDriverExited, "late", "rotating-2");
        assertEquals("late", Credentials.resolveCredential(provider, CALL, driver).get("access").textValue());
        assertTrue(hasDriverExited.get(), "the refresh lock was released while the driver still ran");
        new DexWebRefreshLock(connectionsPath, Duration.ofMillis(300)).acquire().close();
        // The rotated token was stored, so the next caller uses it without another exchange.
        assertEquals("late", Credentials.resolveCredential(provider, CALL, driver).get("access").textValue());
        assertEquals(1, calls.get());
        assertTrue(new String(Files.readAllBytes(connectionsPath), StandardCharsets.UTF_8).contains("rotating-2"));
    }

    @Test
    void driverStillRunningAfterTheGraceWindowNeverResubmitsTheToken() throws Exception {
        writeConnections(credentials("old", "rotating-1"), Instant.now().minusSeconds(60));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final RefreshingProjectCredentialProvider<ObjectNode> provider = shortTimeouts(project,
                Duration.ofMillis(200), Duration.ofMillis(300));
        final AtomicInteger calls = new AtomicInteger();
        final CredentialRefreshDriver<ObjectNode> driver =
                lateDriver(Duration.ofSeconds(3), calls, new AtomicBoolean(), "late", "rotating-2");
        assertThrows(ReauthorizationRequiredException.class, () -> Credentials.resolveCredential(provider, CALL, driver));
        assertThrows(ReauthorizationRequiredException.class, () -> Credentials.resolveCredential(provider, CALL, driver));
        assertEquals(1, calls.get());
        assertEquals("REAUTHORIZATION_REQUIRED",
                project.getConnections().readRecord(KEY).record.get("status").textValue());
    }

    @Test
    void anUnknownOutcomeBlocksOtherStoresAndRestartsFromExchangingTheToken() throws Exception {
        writeConnections(credentials("old", "rotating-1"), Instant.now().minusSeconds(60));
        final LoadedProject first = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final LoadedProject second = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final AtomicInteger firstCalls = new AtomicInteger();
        final CredentialRefreshDriver<ObjectNode> stuck =
                lateDriver(Duration.ofSeconds(3), firstCalls, new AtomicBoolean(), "late", "rotating-2");
        assertThrows(ReauthorizationRequiredException.class, () -> Credentials.resolveCredential(
                shortTimeouts(first, Duration.ofMillis(200), Duration.ofMillis(300)), CALL, stuck));
        final AtomicInteger otherCalls = new AtomicInteger();
        final CredentialRefreshDriver<ObjectNode> counting =
                lateDriver(Duration.ZERO, otherCalls, new AtomicBoolean(), "other", "rotating-3");
        assertThrows(ReauthorizationRequiredException.class,
                () -> Credentials.resolveCredential(provider(second), CALL, counting));
        final LoadedProject restarted = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        assertThrows(ReauthorizationRequiredException.class,
                () -> Credentials.resolveCredential(provider(restarted), CALL, counting));
        assertEquals(0, otherCalls.get());
        assertEquals(1, firstCalls.get());

        // A new authorization saved by Dex Web lifts the block.
        writeConnections(credentials("fresh", "rotating-9"), Instant.now().minusSeconds(60));
        final LoadedProject reauthorized = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        assertEquals("other", Credentials.resolveCredential(provider(reauthorized), CALL, counting)
                .get("access").textValue());
        assertEquals(1, otherCalls.get());
    }

    @Test
    void ambiguousDriverFailuresKeepTheTokenFromBeingExchangedAgain() throws Exception {
        writeConnections(credentials("old", "rotating-1"), Instant.now().minusSeconds(60));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final AtomicInteger calls = new AtomicInteger();
        final CredentialRefreshDriver<ObjectNode> resetAfterSend = failingDriver(calls,
                new ProviderTransportException(false, null, null));
        assertThrows(ReauthorizationRequiredException.class,
                () -> Credentials.resolveCredential(provider(project), CALL, resetAfterSend));
        assertEquals("REAUTHORIZATION_REQUIRED",
                project.getConnections().readRecord(KEY).record.get("status").textValue());
        final LoadedProject other = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        assertThrows(ReauthorizationRequiredException.class,
                () -> Credentials.resolveCredential(provider(other), CALL, resetAfterSend));
        assertEquals(1, calls.get());
    }

    @Test
    void provablyUnsentFailuresRestoreReadyAndAllowAnotherExchange() throws Exception {
        writeConnections(credentials("old", "rotating-1"), Instant.now().minusSeconds(60));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final AtomicInteger calls = new AtomicInteger();
        final CredentialRefreshDriver<ObjectNode> notSent = failingDriver(calls,
                new RetryException(new Failure(FailureKind.TRANSPORT, "sample", "refreshCredentials", "not sent"))
                        .initCause(new ProviderTransportException(true, null, null)));
        assertThrows(RefreshFailedException.class,
                () -> Credentials.resolveCredential(provider(project), CALL, notSent));
        assertEquals("READY", project.getConnections().readRecord(KEY).record.get("status").textValue());
        assertThrows(RefreshFailedException.class,
                () -> Credentials.resolveCredential(provider(project), CALL, notSent));
        assertEquals(2, calls.get());
    }

    @Test
    void repeatedInterruptsDoNotEndTheGraceWait() throws Exception {
        writeConnections(credentials("old", "rotating-1"), Instant.now().minusSeconds(60));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final RefreshingProjectCredentialProvider<ObjectNode> provider = shortTimeouts(project,
                Duration.ofSeconds(5), Duration.ofSeconds(5));
        final AtomicInteger calls = new AtomicInteger();
        final AtomicBoolean hasDriverExited = new AtomicBoolean();
        final CredentialRefreshDriver<ObjectNode> driver =
                lateDriver(Duration.ofMillis(800), calls, hasDriverExited, "late", "rotating-2");
        final AtomicReference<Object> outcome = new AtomicReference<>();
        final AtomicBoolean wasInterruptFlagKept = new AtomicBoolean();
        final Thread caller = new Thread(() -> {
            try {
                outcome.set(Credentials.resolveCredential(provider, CALL, driver));
            } catch (Exception error) {
                outcome.set(error);
            }
            wasInterruptFlagKept.set(Thread.currentThread().isInterrupted());
        });
        caller.start();
        Thread.sleep(100);
        caller.interrupt();
        Thread.sleep(200);
        caller.interrupt();
        caller.join(10_000);
        assertTrue(hasDriverExited.get());
        assertTrue(outcome.get() instanceof ObjectNode, String.valueOf(outcome.get()));
        assertEquals("late", ((ObjectNode) outcome.get()).get("access").textValue());
        assertTrue(wasInterruptFlagKept.get());
        assertEquals(1, calls.get());
    }

    @Test
    void twoLocalStoresOnOneDirectoryKeepEveryInboxEvent() throws Exception {
        final Path directory = temporaryDirectory.resolve("trigger-inboxes");
        final LocalFileObjectStore first = new LocalFileObjectStore(directory);
        final LocalFileObjectStore second = new LocalFileObjectStore(directory);
        first.initialize();
        second.initialize();
        final Scope scope = new Scope("local", "preview");
        final TriggerInboxKey key = new TriggerInboxKey("sample", "main", "itemCreated", "binding");
        new TriggerInbox(first, scope, key).add(new PendingTriggerEvent("Ev1", "{}".getBytes(StandardCharsets.UTF_8)));
        new TriggerInbox(second, scope, key).add(new PendingTriggerEvent("Ev2", "{}".getBytes(StandardCharsets.UTF_8)));
        final LocalFileObjectStore reopened = new LocalFileObjectStore(directory);
        reopened.initialize();
        assertEquals(Arrays.asList("Ev1", "Ev2"), ids(new TriggerInbox(reopened, scope, key)));
        assertEquals(Arrays.asList("Ev1", "Ev2"), ids(new TriggerInbox(first, scope, key)));
    }

    @Test
    void reorderingCredentialMembersKeepsTheDigestAndTheHead() throws Exception {
        assertEquals(DexWebFiles.credentialDigest(material("{\"credentials\":{\"a\":1e0,\"b\":\"x\"}}")),
                DexWebFiles.credentialDigest(material("{\"credentials\":{\"b\":\"x\",\"a\":1e0}}")));
        assertNotEquals(DexWebFiles.credentialDigest(material("{\"credentials\":{\"a\":1e0,\"b\":\"x\"}}")),
                DexWebFiles.credentialDigest(material("{\"credentials\":{\"a\":1,\"b\":\"x\"}}")));
        final Instant expiresAt = Instant.now().plusSeconds(3600);
        writeConnections(credentials("old", "kept"), expiresAt);
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final long revision = project.getConnections().readRecord(KEY).record.get("revision").longValue();
        writeConnections(GoCompatibleJson.newObject().put("refresh", "kept").put("access", "old"), expiresAt);
        assertEquals("old", provider(project).resolve(CALL).get("access").textValue());
        assertEquals(revision, project.getConnections().readRecord(KEY).record.get("revision").longValue());
    }

    @Test
    void localObjectsUseGoEtagsAndKeepOlderEtagsForCompareAndSwap() throws Exception {
        final Path directory = temporaryDirectory.resolve("objects");
        final LocalFileObjectStore store = new LocalFileObjectStore(directory);
        store.initialize();
        final String key = "projects/local/preview/trigger-inboxes/x/head";
        final byte[] contents = "{}".getBytes(StandardCharsets.UTF_8);
        final ProjectObject created = store.createObject(key, contents);
        final String hex = GoCompatibleJson.sha256Hex(contents);
        assertEquals(hex + "-1", created.getEtag());
        final Path file = directory.resolve(GoCompatibleJson.sha256Hex(key.getBytes(StandardCharsets.UTF_8)) + "-1.json");
        assertArrayEquals(("{\"key\":\"" + key + "\",\"version\":\"1\",\"etag\":\"" + hex + "-1\",\"contents\":\""
                + Base64.getEncoder().encodeToString(contents) + "\"}").getBytes(StandardCharsets.UTF_8),
                Files.readAllBytes(file));

        final String oldKey = "projects/local/preview/trigger-inboxes/old/head";
        final Path oldFile = directory.resolve(
                GoCompatibleJson.sha256Hex(oldKey.getBytes(StandardCharsets.UTF_8)) + "-3.json");
        Files.write(oldFile, ("{\"key\":\"" + oldKey + "\",\"version\":\"3\",\"etag\":\"sha256:ab-3\",\"contents\":\"e30=\"}")
                .getBytes(StandardCharsets.UTF_8));
        Files.setPosixFilePermissions(oldFile, PosixFilePermissions.fromString("rw-------"));
        // A file written by an older release is migrated when a store initializes the directory.
        final LocalFileObjectStore reopened = new LocalFileObjectStore(directory);
        reopened.initialize();
        assertEquals("sha256:ab-3", reopened.readObject(oldKey, "").getEtag());
        final ProjectObject swapped = reopened.compareAndSwapObject(oldKey, "sha256:ab-3", contents);
        assertEquals(hex + "-4", swapped.getEtag());
    }

    private static java.util.List<String> ids(final TriggerInbox inbox) throws IOException {
        final java.util.List<String> ids = new java.util.ArrayList<>();
        for (PendingTriggerEvent event : inbox.pending()) {
            ids.add(event.getId());
        }
        return ids;
    }

    private static CredentialMaterial material(final String document) {
        final ObjectNode parsed = (ObjectNode) GoCompatibleJson.parse(document.getBytes(StandardCharsets.UTF_8));
        return new CredentialMaterial((ObjectNode) parsed.get("credentials"), Rfc3339NanoTimestamp.ZERO, "oauth2");
    }

    private static ObjectNode credentials(final String access, final String refresh) {
        return GoCompatibleJson.newObject().put("access", access).put("refresh", refresh);
    }

    private void writeConnections(final ObjectNode credentials, final Instant expiresAt) throws IOException {
        final ObjectNode document = GoCompatibleJson.newObject();
        document.put("schemaVersion", "connectors.dex.dev/local-connections/v1alpha1");
        final ObjectNode connection = document.putArray("connections").addObject();
        connection.put("connectorId", "sample").put("connectionName", "main").put("modulePath", "example")
                .put("provider", "sample").putObject("configuration");
        connection.set("credentials", credentials);
        connection.put("credentialExpiresAt", Rfc3339NanoTimestamp.fromInstant(expiresAt).toWire());
        document.putArray("triggerBindings");
        Files.write(connectionsPath, MAPPER.writeValueAsBytes(document));
        Files.setPosixFilePermissions(connectionsPath, PosixFilePermissions.fromString("rw-------"));
    }

    private static RefreshingProjectCredentialProvider<ObjectNode> shortTimeouts(
            final LoadedProject project, final Duration refreshTimeout, final Duration driverExitGrace) {
        return new RefreshingProjectCredentialProvider<>(project.getConnections(), KEY, ObjectNode::deepCopy,
                ObjectNode::deepCopy, null, refreshTimeout, driverExitGrace);
    }

    /** A driver that ignores cancellation and rotates the token after the given time. */
    private static CredentialRefreshDriver<ObjectNode> lateDriver(
            final Duration runFor, final AtomicInteger calls, final AtomicBoolean hasExited,
            final String access, final String refresh) {
        return new ExpiryOnlyDriver() {
            @Override
            public Result<ObjectNode> refresh(final State<ObjectNode> state) {
                calls.incrementAndGet();
                final long until = System.nanoTime() + runFor.toNanos();
                while (System.nanoTime() < until) {
                    try {
                        Thread.sleep(20);
                    } catch (InterruptedException ignored) {
                        // This driver deliberately ignores cancellation.
                    }
                }
                hasExited.set(true);
                return new Result<>(credentials(access, refresh), Instant.now().plusSeconds(3600));
            }
        };
    }

    /** A driver that fails every exchange with the given error. */
    private static CredentialRefreshDriver<ObjectNode> failingDriver(final AtomicInteger calls, final Throwable error) {
        return new ExpiryOnlyDriver() {
            @Override
            public Result<ObjectNode> refresh(final State<ObjectNode> state) throws IOException {
                calls.incrementAndGet();
                if (error instanceof IOException) {
                    throw (IOException) error;
                }
                throw (RuntimeException) error;
            }
        };
    }

    private static RefreshingProjectCredentialProvider<ObjectNode> provider(final LoadedProject project) {
        return new RefreshingProjectCredentialProvider<>(project.getConnections(), KEY, ObjectNode::deepCopy,
                ObjectNode::deepCopy);
    }

    /** Refreshes only expired material. */
    private abstract static class ExpiryOnlyDriver implements CredentialRefreshDriver<ObjectNode> {
        @Override
        public boolean isRefreshRequired(final State<ObjectNode> state) {
            return false;
        }
    }
}
