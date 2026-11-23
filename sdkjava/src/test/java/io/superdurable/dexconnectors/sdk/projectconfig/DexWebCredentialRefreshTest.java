// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.CredentialRefreshDriver;
import io.superdurable.dexconnectors.sdk.Credentials;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.BufferedReader;
import java.io.File;
import java.io.IOException;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.nio.file.attribute.PosixFilePermissions;
import java.time.Duration;
import java.time.Instant;
import java.util.Comparator;
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

class DexWebCredentialRefreshTest {
    private static final ConnectionKey KEY = new ConnectionKey("sample", "main");
    private static final Call CALL = Call.newTriggerCall(new ConnectionRef("sample", "main"),
            new OperationRef("sample", "createItem"), CallId.of("5af8fafa-06ff-5b2d-bb13-04693809fabf"));

    private Path temporaryDirectory;
    private Path connectionsPath;
    private final ExecutorService executor = Executors.newCachedThreadPool();

    @BeforeEach
    void createTemporaryDirectory() throws IOException {
        temporaryDirectory = Files.createTempDirectory(Paths.get("/tmp"), "sdkjava-dexweb-");
        connectionsPath = temporaryDirectory.resolve("connections.json");
    }

    @AfterEach
    void cleanUp() throws IOException {
        executor.shutdownNow();
        Files.setPosixFilePermissions(temporaryDirectory, PosixFilePermissions.fromString("rwx------"));
        try (Stream<Path> paths = Files.walk(temporaryDirectory)) {
            paths.sorted(Comparator.reverseOrder()).forEach(path -> path.toFile().delete());
        }
    }

    @Test
    void twoIndependentStoresOnOneFileRefreshOnce() throws Exception {
        writeConnections("old", "rotating-1", Instant.now().minusSeconds(60));
        final LoadedProject first = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final LoadedProject second = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final RotatingDriver driver = new RotatingDriver();
        final CountDownLatch start = new CountDownLatch(1);
        final Future<ObjectNode> firstResult = executor.submit(() -> {
            start.await();
            return Credentials.resolveCredential(provider(first), CALL, driver);
        });
        final Future<ObjectNode> secondResult = executor.submit(() -> {
            start.await();
            return Credentials.resolveCredential(provider(second), CALL, driver);
        });
        start.countDown();
        assertEquals("new-1", firstResult.get(30, TimeUnit.SECONDS).get("access").textValue());
        assertEquals("new-1", secondResult.get(30, TimeUnit.SECONDS).get("access").textValue());
        assertEquals(1, driver.calls.get());
        assertEquals("rotating-2", MAPPER.readTree(connectionsPath.toFile()).get("connections").get(0)
                .get("credentials").get("refresh").textValue());
        assertTrue(Files.isRegularFile(temporaryDirectory.resolve("connections.json.lock")));
    }

    @Test
    void failedWriteBackPrecheckMakesNoProviderRequest() throws Exception {
        writeConnections("old", "rotating-1", Instant.now().minusSeconds(60));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final RotatingDriver driver = new RotatingDriver();
        Files.setPosixFilePermissions(temporaryDirectory, PosixFilePermissions.fromString("r-x------"));
        assertThrows(RefreshFailedException.class,
                () -> Credentials.resolveCredential(provider(project), CALL, driver));
        assertEquals(0, driver.calls.get());
        Files.setPosixFilePermissions(temporaryDirectory, PosixFilePermissions.fromString("rwx------"));
        assertEquals("new-1", Credentials.resolveCredential(provider(project), CALL, driver).get("access").textValue());
        assertEquals(1, driver.calls.get());
    }

    @Test
    void deletedConnectionStopsResolvingAndARecreatedOneResolvesItsNewCredential() throws Exception {
        writeConnections("old", "kept", Instant.now().plusSeconds(3600));
        final LoadedProject project = ProjectConfigurationLoader.loadDexWebProject(connectionsPath);
        final RefreshingProjectCredentialProvider<ObjectNode> provider = provider(project);
        final RotatingDriver driver = new RotatingDriver();
        assertEquals("old", Credentials.resolveCredential(provider, CALL, driver).get("access").textValue());
        writeConnectionsWithout();
        assertThrows(ConnectionRemovedFromFileException.class,
                () -> Credentials.resolveCredential(provider, CALL, driver));
        assertThrows(ConnectionRemovedFromFileException.class, () -> provider.resolve(CALL));
        writeConnections("recreated", "kept", Instant.now().plusSeconds(3600));
        assertEquals("recreated", Credentials.resolveCredential(provider, CALL, driver).get("access").textValue());
        writeConnectionsWithout();
        writeConnections("old", "kept", Instant.now().plusSeconds(3600));
        assertEquals("old", Credentials.resolveCredential(provider, CALL, driver).get("access").textValue());
        assertEquals(0, driver.calls.get());
    }

    @Test
    void anotherProcessHoldingTheLockFileExcludesThisProcess() throws Exception {
        writeConnections("old", "kept", Instant.now().plusSeconds(3600));
        final String java = Paths.get(System.getProperty("java.home"), "bin", "java").toString();
        final Process child = new ProcessBuilder(java, "-cp", System.getProperty("java.class.path"),
                DexWebRefreshLockHolder.class.getName(), connectionsPath.toString())
                .redirectError(ProcessBuilder.Redirect.INHERIT).start();
        try {
            final BufferedReader output = new BufferedReader(
                    new InputStreamReader(child.getInputStream(), StandardCharsets.UTF_8));
            assertEquals("locked", output.readLine());
            final DexWebRefreshLock lock = new DexWebRefreshLock(connectionsPath, Duration.ofMillis(300));
            assertThrows(RefreshFailedException.class, lock::acquire);
            try (OutputStream input = child.getOutputStream()) {
                input.write('\n');
            }
            assertTrue(child.waitFor(30, TimeUnit.SECONDS));
            lock.acquire().close();
        } finally {
            child.destroyForcibly();
        }
    }

    private void writeConnections(final String access, final String refresh, final Instant expiresAt) throws IOException {
        final ObjectNode document = GoCompatibleJson.newObject();
        document.put("schemaVersion", "connectors.dex.dev/local-connections/v1alpha1");
        final ObjectNode connection = document.putArray("connections").addObject();
        connection.put("connectorId", "sample").put("connectionName", "main").put("modulePath", "example")
                .put("provider", "sample").putObject("configuration");
        connection.set("credentials", GoCompatibleJson.newObject().put("access", access).put("refresh", refresh));
        connection.put("credentialExpiresAt", Rfc3339NanoTimestamp.fromInstant(expiresAt).toWire());
        document.putArray("triggerBindings");
        writeOwnerOnly(MAPPER.writeValueAsBytes(document));
    }

    private void writeConnectionsWithout() throws IOException {
        final ObjectNode document = GoCompatibleJson.newObject();
        document.put("schemaVersion", "connectors.dex.dev/local-connections/v1alpha1");
        document.putArray("connections");
        document.putArray("triggerBindings");
        writeOwnerOnly(MAPPER.writeValueAsBytes(document));
    }

    private void writeOwnerOnly(final byte[] contents) throws IOException {
        final File file = connectionsPath.toFile();
        Files.write(connectionsPath, contents);
        Files.setPosixFilePermissions(file.toPath(), PosixFilePermissions.fromString("rw-------"));
    }

    private static RefreshingProjectCredentialProvider<ObjectNode> provider(final LoadedProject project) {
        return new RefreshingProjectCredentialProvider<>(project.getConnections(), KEY, ObjectNode::deepCopy,
                ObjectNode::deepCopy);
    }

    /** Rotates the refresh token on every exchange and counts provider requests. */
    private static final class RotatingDriver implements CredentialRefreshDriver<ObjectNode> {
        private final AtomicInteger calls = new AtomicInteger();

        @Override
        public boolean isRefreshRequired(final State<ObjectNode> state) {
            return false;
        }

        @Override
        public Result<ObjectNode> refresh(final State<ObjectNode> state) {
            final int call = calls.incrementAndGet();
            return new Result<>(GoCompatibleJson.newObject().put("access", "new-" + call)
                    .put("refresh", "rotating-" + (call + 1)), Instant.now().plusSeconds(3600));
        }
    }
}
