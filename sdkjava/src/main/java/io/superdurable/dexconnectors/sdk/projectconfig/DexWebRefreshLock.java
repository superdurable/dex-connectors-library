// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.nio.file.attribute.BasicFileAttributes;
import java.time.Duration;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.concurrent.Semaphore;
import java.util.concurrent.TimeUnit;

/**
 * The cross-process credential refresh lock of a Dex Web {@code connections.json}: an exclusive lock
 * on the never-replaced {@code connections.json.lock} file beside it, as Go's SDK uses.
 *
 * <p>Within one JVM a semaphore per lock path orders every store of that file, because a JVM holds
 * a file lock for the whole process. Across processes the JDK takes a POSIX {@code fcntl} lock.
 * On macOS and other BSD systems that lock also excludes Go's {@code flock(2)}; on Linux the two
 * lock kinds are independent, so Java processes exclude each other but not a Go process using the
 * same file there.
 */
final class DexWebRefreshLock {
    static final String LOCK_FILE_SUFFIX = ".lock";
    static final Duration MAXIMUM_WAIT = Duration.ofMinutes(2);

    private static final ConcurrentMap<Path, Semaphore> SEMAPHORES = new ConcurrentHashMap<>();
    private static final long FIRST_POLL_DELAY_MILLISECONDS = 5;
    private static final long MAXIMUM_POLL_DELAY_MILLISECONDS = 100;

    private final Path lockPath;
    private final Duration maximumWait;
    private final Semaphore semaphore;

    DexWebRefreshLock(final Path connectionsPath, final Duration maximumWait) {
        this.lockPath = connectionsPath.toAbsolutePath().resolveSibling(
                connectionsPath.getFileName().toString() + LOCK_FILE_SUFFIX);
        this.maximumWait = maximumWait;
        this.semaphore = SEMAPHORES.computeIfAbsent(lockPath, ignored -> new Semaphore(1));
    }

    Path getLockPath() {
        return lockPath;
    }

    /** Waits for the in-process semaphore, then the file lock, until the maximum wait passes. */
    Held acquire() throws IOException, InterruptedException {
        final long deadline = System.nanoTime() + maximumWait.toNanos();
        if (!semaphore.tryAcquire(maximumWait.toNanos(), TimeUnit.NANOSECONDS)) {
            throw new RefreshFailedException("wait for the credential refresh lock timed out");
        }
        FileChannel channel = null;
        try {
            channel = FileChannel.open(lockPath, StandardOpenOption.CREATE, StandardOpenOption.READ,
                    StandardOpenOption.WRITE, LinkOption.NOFOLLOW_LINKS);
            requirePrivateLockFile();
            long delay = FIRST_POLL_DELAY_MILLISECONDS;
            while (true) {
                final FileLock lock = channel.tryLock();
                if (lock != null) {
                    final Held held = new Held(channel, semaphore);
                    channel = null;
                    return held;
                }
                if (System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(delay) > deadline) {
                    throw new RefreshFailedException("wait for the credential refresh lock timed out");
                }
                Thread.sleep(delay);
                delay = Math.min(2 * delay, MAXIMUM_POLL_DELAY_MILLISECONDS);
            }
        } catch (IOException | InterruptedException | RuntimeException error) {
            if (channel != null) {
                closeQuietly(channel);
            }
            semaphore.release();
            if (error instanceof RefreshFailedException || error instanceof InterruptedException) {
                throw error;
            }
            throw new RefreshFailedException("the credential refresh lock file cannot be locked");
        }
    }

    private void requirePrivateLockFile() throws IOException {
        final BasicFileAttributes attributes =
                Files.readAttributes(lockPath, BasicFileAttributes.class, LinkOption.NOFOLLOW_LINKS);
        if (!attributes.isRegularFile()) {
            throw new RefreshFailedException("the credential refresh lock file is not a regular file");
        }
        DexWebFiles.setOwnerOnlyMode(lockPath);
    }

    private static void closeQuietly(final FileChannel channel) {
        try {
            channel.close();
        } catch (IOException ignored) {
            // The channel held no lock, so a failed close changes nothing.
        }
    }

    /** One acquisition; closing releases the file lock, then the semaphore. */
    static final class Held implements AutoCloseable {
        private final FileChannel channel;
        private final Semaphore semaphore;
        private boolean isReleased;

        private Held(final FileChannel channel, final Semaphore semaphore) {
            this.channel = channel;
            this.semaphore = semaphore;
        }

        @Override
        public synchronized void close() {
            if (isReleased) {
                return;
            }
            isReleased = true;
            // Closing the channel releases its lock even when close reports an error.
            closeQuietly(channel);
            semaphore.release();
        }
    }
}
