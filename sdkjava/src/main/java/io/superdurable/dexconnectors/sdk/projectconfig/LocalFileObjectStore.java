// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.charset.StandardCharsets;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.NoSuchFileException;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.util.ArrayList;
import java.util.Base64;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.concurrent.locks.ReentrantLock;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Versioned objects kept as owner-only files in the Go and TypeScript local layout.
 *
 * <p>Each object is {@code <sha256 of key>-<version>.json} holding its key, version, ETag, and
 * base64 contents; new ETags are {@code <sha256 hex of contents>-<version>}, as Go writes them,
 * and an ETag read from an older file is kept as its compare-and-swap precondition. Only the latest
 * version of each key is kept.
 *
 * <p>Java also keeps a {@code <sha256 of key>.head} pointer holding the latest version, so reads
 * and writes open one key's files directly instead of scanning the directory. {@link #initialize()}
 * enumerates the directory once, keeps the newest version of each key, deletes crash leftovers,
 * and writes any missing or stale pointer, which also migrates directories written without them.
 * A pointer that lags after a crash is advanced by probing the next versions.
 *
 * <p>Go keeps this store in one application process. Java reads the current version from disk
 * under a directory lock for every read and write, so several stores and Java processes on one
 * directory never overwrite each other's writes: the lock is a JVM-wide lock per real directory
 * plus a {@code .objects.lock} file lock. That file lock does not exclude Go, Rust, or Python
 * processes on Linux, so never share one local directory across languages there. Thread-safe.
 */
final class LocalFileObjectStore implements ObjectStore {
    private static final Pattern LOCAL_VERSION = Pattern.compile("^[1-9][0-9]*$");
    private static final Pattern VERSION_FILE_NAME = Pattern.compile("^([0-9a-f]{64})-([1-9][0-9]*)\\.json$");
    private static final String LOCK_FILE_NAME = ".objects.lock";
    private static final String HEAD_SUFFIX = ".head";
    private static final ConcurrentMap<Path, ReentrantLock> DIRECTORY_LOCKS = new ConcurrentHashMap<>();

    private final Path directory;

    LocalFileObjectStore(final Path directory) {
        this.directory = directory.toAbsolutePath();
    }

    void initialize() throws IOException {
        Files.createDirectories(directory);
        final DirectoryLock lock = lockDirectory();
        try {
            final Map<String, List<ProjectObject>> versionsByKey = new HashMap<>();
            try (DirectoryStream<Path> files = Files.newDirectoryStream(directory, "*.json")) {
                for (Path path : files) {
                    final String name = path.getFileName().toString();
                    if (name.startsWith(".")) {
                        continue;
                    }
                    // Every object file is validated, as before, even when its name is not a version file.
                    final ProjectObject item = decodeLocalObject(Files.readAllBytes(path));
                    final Matcher match = VERSION_FILE_NAME.matcher(name);
                    if (match.matches() && match.group(1).equals(keyHash(item.getKey()))
                            && match.group(2).equals(item.getVersion())) {
                        versionsByKey.computeIfAbsent(item.getKey(), ignored -> new ArrayList<>()).add(item);
                    }
                }
            }
            for (Map.Entry<String, List<ProjectObject>> entry : versionsByKey.entrySet()) {
                final ProjectObject newest = keepNewest(entry.getValue());
                if (!newest.getVersion().equals(readHeadPointer(entry.getKey()))) {
                    writeHeadPointer(entry.getKey(), newest.getVersion());
                }
            }
        } finally {
            lock.close();
        }
    }

    @Override
    public ProjectObject readObject(final String key, final String version) throws IOException {
        final ProjectObject item;
        final DirectoryLock lock = lockDirectory();
        try {
            item = latest(key);
        } finally {
            lock.close();
        }
        if (item == null || (version != null && !version.isEmpty() && !item.getVersion().equals(version))) {
            throw new ObjectNotFoundException("project object not found");
        }
        return item;
    }

    @Override
    public ProjectObject createObject(final String key, final byte[] contents) throws IOException {
        final DirectoryLock lock = lockDirectory();
        try {
            if (latest(key) != null) {
                throw new ObjectConflictException("project object already exists");
            }
            return write(key, contents, null);
        } finally {
            lock.close();
        }
    }

    @Override
    public ProjectObject compareAndSwapObject(
            final String key, final String expectedEtag, final byte[] contents) throws IOException {
        final DirectoryLock lock = lockDirectory();
        try {
            final ProjectObject current = latest(key);
            if (current == null) {
                throw new ObjectNotFoundException("project object not found");
            }
            if (expectedEtag == null || expectedEtag.isEmpty() || !current.getEtag().equals(expectedEtag)) {
                throw new ObjectConflictException("project object compare-and-swap conflict");
            }
            return write(key, contents, current);
        } finally {
            lock.close();
        }
    }

    private ProjectObject write(final String key, final byte[] contents, final ProjectObject previous)
            throws IOException {
        // The new version and its pointer are durable before the superseded file is deleted.
        InMemoryObjectStore.validateObjectKey(key);
        if (contents == null || contents.length == 0
                || contents.length > InMemoryObjectStore.localObjectByteLimit(key)) {
            throw new IllegalArgumentException("project object body is empty or oversized");
        }
        final String version = Long.toString(previous == null ? 1 : Long.parseLong(previous.getVersion()) + 1);
        final ProjectObject item = new ProjectObject(key, version,
                GoCompatibleJson.sha256Hex(contents) + "-" + version, contents);
        final ObjectNode document = GoCompatibleJson.newObject();
        document.put("key", key);
        document.put("version", version);
        document.put("etag", item.getEtag());
        document.put("contents", Base64.getEncoder().encodeToString(contents));
        DexWebFiles.writePrivateFile(file(key, version), GoCompatibleJson.encode(document));
        writeHeadPointer(key, version);
        if (previous != null) {
            removeFile(key, previous.getVersion());
        }
        return item;
    }

    /** Reads the newest version of a key through its head pointer; call under the lock. */
    private ProjectObject latest(final String key) throws IOException {
        final String pointer = readHeadPointer(key);
        if (pointer == null) {
            // Every Java write and initialize() records a pointer, so a key without one does not exist.
            return null;
        }
        long version = Long.parseLong(pointer);
        ProjectObject newest = readVersion(key, version);
        // A crash after writing a version but before its pointer leaves the pointer one behind.
        while (true) {
            final ProjectObject next = readVersion(key, version + 1);
            if (next == null) {
                break;
            }
            if (newest != null) {
                removeFile(key, newest.getVersion());
            }
            newest = next;
            version++;
        }
        if (newest == null) {
            return null;
        }
        if (!newest.getVersion().equals(pointer)) {
            writeHeadPointer(key, newest.getVersion());
        }
        return newest;
    }

    private ProjectObject readVersion(final String key, final long version) throws IOException {
        final byte[] contents;
        try {
            contents = Files.readAllBytes(file(key, Long.toString(version)));
        } catch (NoSuchFileException missing) {
            return null;
        }
        final ProjectObject item = decodeLocalObject(contents);
        if (!item.getKey().equals(key) || !item.getVersion().equals(Long.toString(version))) {
            throw new IllegalArgumentException("local project storage file is invalid");
        }
        return item;
    }

    private ProjectObject keepNewest(final List<ProjectObject> versions) {
        versions.sort((left, right) -> Long.compare(Long.parseLong(left.getVersion()),
                Long.parseLong(right.getVersion())));
        // A crash between writing a new version and deleting the old one leaves both.
        for (ProjectObject stale : versions.subList(0, versions.size() - 1)) {
            removeFile(stale.getKey(), stale.getVersion());
        }
        return versions.get(versions.size() - 1);
    }

    private String readHeadPointer(final String key) throws IOException {
        final byte[] contents;
        try {
            contents = Files.readAllBytes(headPointer(key));
        } catch (NoSuchFileException missing) {
            return null;
        }
        final String version = new String(contents, StandardCharsets.US_ASCII);
        if (!LOCAL_VERSION.matcher(version).matches()) {
            throw new IllegalArgumentException("local project storage file is invalid");
        }
        return version;
    }

    private void writeHeadPointer(final String key, final String version) throws IOException {
        DexWebFiles.writePrivateFile(headPointer(key), version.getBytes(StandardCharsets.US_ASCII));
    }

    private DirectoryLock lockDirectory() throws IOException {
        Files.createDirectories(directory);
        final Path realDirectory = directory.toRealPath();
        final ReentrantLock processLock = DIRECTORY_LOCKS.computeIfAbsent(realDirectory, ignored -> new ReentrantLock());
        processLock.lock();
        FileChannel channel = null;
        boolean isHandedOff = false;
        try {
            channel = FileChannel.open(realDirectory.resolve(LOCK_FILE_NAME), StandardOpenOption.CREATE,
                    StandardOpenOption.READ, StandardOpenOption.WRITE, LinkOption.NOFOLLOW_LINKS);
            DexWebFiles.setOwnerOnlyMode(realDirectory.resolve(LOCK_FILE_NAME));
            final FileLock fileLock = channel.lock();
            final DirectoryLock held = new DirectoryLock(processLock, channel, fileLock);
            isHandedOff = true;
            return held;
        } finally {
            // Every failure, including a channel that never opened, releases the JVM lock.
            if (!isHandedOff) {
                try {
                    if (channel != null) {
                        channel.close();
                    }
                } finally {
                    processLock.unlock();
                }
            }
        }
    }

    private Path file(final String key, final String version) {
        return directory.resolve(keyHash(key) + "-" + version + ".json");
    }

    private Path headPointer(final String key) {
        return directory.resolve(keyHash(key) + HEAD_SUFFIX);
    }

    private static String keyHash(final String key) {
        return GoCompatibleJson.sha256Hex(key.getBytes(StandardCharsets.UTF_8));
    }

    private void removeFile(final String key, final String version) {
        try {
            Files.deleteIfExists(file(key, version));
        } catch (IOException error) {
            // A failure leaves a stale file that the next initialize() discards.
        }
    }

    private static ProjectObject decodeLocalObject(final byte[] contents) {
        try {
            final ObjectNode record = GoCompatibleJson.parseObject(contents, "local object");
            final JsonNode key = record.get("key");
            final JsonNode version = record.get("version");
            final JsonNode etag = record.get("etag");
            final JsonNode encoded = record.get("contents");
            if (key == null || !key.isTextual() || key.textValue().isEmpty() || etag == null || !etag.isTextual()
                    || etag.textValue().isEmpty() || version == null || !version.isTextual()
                    || !LOCAL_VERSION.matcher(version.textValue()).matches()
                    || encoded == null || !encoded.isTextual()) {
                throw new IllegalArgumentException("invalid fields");
            }
            return new ProjectObject(key.textValue(), version.textValue(), etag.textValue(),
                    Base64.getDecoder().decode(encoded.textValue()));
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("local project storage file is invalid");
        }
    }

    /** The JVM lock and file lock of one directory; closing releases both. */
    private static final class DirectoryLock implements AutoCloseable {
        private final ReentrantLock processLock;
        private final FileChannel channel;
        private final FileLock fileLock;

        private DirectoryLock(final ReentrantLock processLock, final FileChannel channel, final FileLock fileLock) {
            this.processLock = processLock;
            this.channel = channel;
            this.fileLock = fileLock;
        }

        @Override
        public void close() throws IOException {
            try {
                try {
                    fileLock.release();
                } finally {
                    channel.close();
                }
            } finally {
                processLock.unlock();
            }
        }
    }
}
