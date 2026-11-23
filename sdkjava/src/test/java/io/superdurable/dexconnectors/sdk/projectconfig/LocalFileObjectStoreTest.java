// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Comparator;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.stream.Stream;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

/** Local object files: lock cleanup, head lookup, migration, and scaling; elapsed time is measured. */
class LocalFileObjectStoreTest {
    private static final byte[] CONTENTS = "{}".getBytes(StandardCharsets.UTF_8);

    private Path directory;
    private final ExecutorService executor = Executors.newCachedThreadPool();

    @BeforeEach
    void createTemporaryDirectory() throws IOException {
        directory = Files.createTempDirectory(Paths.get("/tmp"), "sdkjava-local-objects-");
    }

    @AfterEach
    void cleanUp() throws IOException {
        executor.shutdownNow();
        try (Stream<Path> paths = Files.walk(directory)) {
            paths.sorted(Comparator.reverseOrder()).forEach(path -> path.toFile().delete());
        }
    }

    @Test
    void failedLockFileOpenReleasesTheProcessLockForARetry() throws Exception {
        final Path lockFile = directory.resolve(".objects.lock");
        Files.createDirectory(lockFile);
        assertThrows(IOException.class, () -> new LocalFileObjectStore(directory).initialize());
        Files.delete(lockFile);
        final Future<ProjectObject> retry = executor.submit(() -> {
            final LocalFileObjectStore store = new LocalFileObjectStore(directory);
            store.initialize();
            return store.createObject("projects/local/preview/poll-cursors/a/head", CONTENTS);
        });
        assertEquals("1", retry.get(10, TimeUnit.SECONDS).getVersion());
    }

    @Test
    void crashLeftoversAndLaggingPointersRecover() throws Exception {
        final LocalFileObjectStore store = new LocalFileObjectStore(directory);
        store.initialize();
        final String key = "projects/local/preview/trigger-inboxes/a/head";
        final ProjectObject first = store.createObject(key, CONTENTS);
        final byte[] firstFile = Files.readAllBytes(versionFile(key, "1"));
        final ProjectObject second = store.compareAndSwapObject(key, first.getEtag(),
                "{\"a\":1}".getBytes(StandardCharsets.UTF_8));
        // Simulate a crash after writing version 2 but before its pointer and the old file's removal.
        Files.write(versionFile(key, "1"), firstFile);
        Files.write(headPointer(key), "1".getBytes(StandardCharsets.US_ASCII));
        assertEquals(second.getEtag(), store.readObject(key, "").getEtag());
        assertFalse(Files.exists(versionFile(key, "1")));
        assertArrayEquals("2".getBytes(StandardCharsets.US_ASCII), Files.readAllBytes(headPointer(key)));

        // A directory written without pointers is migrated by initialize().
        Files.delete(headPointer(key));
        final LocalFileObjectStore reopened = new LocalFileObjectStore(directory);
        reopened.initialize();
        assertEquals(second.getEtag(), reopened.readObject(key, "").getEtag());
        assertTrue(Files.exists(headPointer(key)));
        assertThrows(ObjectNotFoundException.class,
                () -> reopened.readObject("projects/local/preview/trigger-inboxes/missing/head", ""));
    }

    @Test
    void aFirstVersionLeftWithoutAHeadIsNeverOverwritten() throws Exception {
        final LocalFileObjectStore store = new LocalFileObjectStore(directory);
        store.initialize();
        final String key = "projects/local/preview/trigger-inboxes/first/head";
        final ProjectObject original = store.createObject(key, "{\"event\":1}".getBytes(StandardCharsets.UTF_8));
        // Simulate a crash after writing version 1 but before publishing its head, seen by a live store.
        Files.delete(headPointer(key));
        assertThrows(ObjectConflictException.class,
                () -> store.createObject(key, "{\"event\":2}".getBytes(StandardCharsets.UTF_8)));
        assertArrayEquals(original.getContents(), store.readObject(key, "").getContents());
        assertArrayEquals("1".getBytes(StandardCharsets.US_ASCII), Files.readAllBytes(headPointer(key)));
    }

    @Test
    void corruptOrDanglingHeadsRecoverFromVersionFiles() throws Exception {
        final LocalFileObjectStore store = new LocalFileObjectStore(directory);
        store.initialize();
        final String key = "projects/local/preview/trigger-inboxes/corrupt/head";
        final ProjectObject first = store.createObject(key, CONTENTS);
        final ProjectObject second = store.compareAndSwapObject(key, first.getEtag(),
                "{\"a\":1}".getBytes(StandardCharsets.UTF_8));
        Files.write(headPointer(key), "not-a-version".getBytes(StandardCharsets.US_ASCII));
        final LocalFileObjectStore reopened = new LocalFileObjectStore(directory);
        reopened.initialize();
        assertEquals(second.getEtag(), reopened.readObject(key, "").getEtag());

        Files.write(headPointer(key), "9".getBytes(StandardCharsets.US_ASCII));
        assertEquals(second.getEtag(), store.readObject(key, "").getEtag());
        assertArrayEquals("2".getBytes(StandardCharsets.US_ASCII), Files.readAllBytes(headPointer(key)));

        Files.write(headPointer(key), "garbage".getBytes(StandardCharsets.US_ASCII));
        assertEquals("3", store.compareAndSwapObject(key, second.getEtag(), CONTENTS).getVersion());
        assertFalse(Files.exists(versionFile(key, "2")));
    }

    @Test
    void initializationAndCompareAndSwapDoNotScaleWithUnrelatedObjects() throws Exception {
        final LocalFileObjectStore store = new LocalFileObjectStore(directory);
        store.initialize();
        for (int index = 0; index < 5_000; index++) {
            store.createObject("projects/local/preview/poll-cursors/item-" + index + "/head", CONTENTS);
        }
        final long initializeStartedAt = System.nanoTime();
        final LocalFileObjectStore reopened = new LocalFileObjectStore(directory);
        reopened.initialize();
        final long initializeMillis = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - initializeStartedAt);
        assertTrue(initializeMillis < 2_000, "initialize took " + initializeMillis + " ms for 5,000 objects");

        final String key = "projects/local/preview/poll-cursors/item-42/head";
        String etag = reopened.readObject(key, "").getEtag();
        final long swapStartedAt = System.nanoTime();
        for (int index = 0; index < 50; index++) {
            etag = reopened.compareAndSwapObject(key, etag, ("{\"n\":" + index + "}").getBytes(StandardCharsets.UTF_8))
                    .getEtag();
        }
        final long swapMillis = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - swapStartedAt);
        assertTrue(swapMillis < 1_000, "50 compare-and-swaps took " + swapMillis + " ms beside 5,000 objects");
    }

    private Path versionFile(final String key, final String version) {
        return directory.resolve(GoCompatibleJson.sha256Hex(key.getBytes(StandardCharsets.UTF_8)) + "-" + version + ".json");
    }

    private Path headPointer(final String key) {
        return directory.resolve(GoCompatibleJson.sha256Hex(key.getBytes(StandardCharsets.UTF_8)) + ".head");
    }
}
