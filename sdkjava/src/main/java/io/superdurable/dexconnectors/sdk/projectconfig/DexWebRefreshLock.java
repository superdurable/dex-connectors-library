// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;
import java.nio.channels.FileChannel;
import java.nio.channels.FileLock;
import java.nio.channels.OverlappingFileLockException;
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
 * <p>The lock is identified by the canonical real parent directory, so symbolic-link and
 * {@code ..} aliases of one file share it. Within one JVM a semaphore per lock orders every store
 * of that file, because a JVM holds a file lock for the whole process; acquiring it again on the
 * thread that holds it is rejected rather than waiting for itself.
 *
 * <p>Across processes the JDK takes a POSIX {@code fcntl} lock. On macOS and other BSD systems that
 * lock also excludes Go's {@code flock(2)}. On Linux the two lock kinds are independent, so Java
 * processes exclude each other but not a Go, Rust, or Python process: on Linux, never share one
 * local Dex Web {@code connections.json} between Java and processes of another language.
 */
final class DexWebRefreshLock {
    static final String LOCK_FILE_SUFFIX = ".lock";
    static final Duration MAXIMUM_WAIT = Duration.ofMinutes(2);

    private static final ConcurrentMap<Path, Semaphore> SEMAPHORES = new ConcurrentHashMap<>();
    private static final ConcurrentMap<Path, Thread> OWNERS = new ConcurrentHashMap<>();
    private static final long FIRST_POLL_DELAY_MILLISECONDS = 5;
    private static final long MAXIMUM_POLL_DELAY_MILLISECONDS = 100;

    private final Path lockPath;
    private final Duration maximumWait;
    private final Semaphore semaphore;

    DexWebRefreshLock(final Path connectionsPath, final Duration maximumWait) {
        this.lockPath = canonicalLockPath(connectionsPath);
        this.maximumWait = maximumWait;
        this.semaphore = SEMAPHORES.computeIfAbsent(lockPath, ignored -> new Semaphore(1));
    }

    Path getLockPath() {
        return lockPath;
    }

    /**
     * Waits for the in-process semaphore, then the file lock, until the maximum wait passes.
     *
     * @throws IllegalStateException if the calling thread already holds this lock
     */
    Held acquire() throws IOException, InterruptedException {
        if (OWNERS.get(lockPath) == Thread.currentThread()) {
            throw new IllegalStateException("the credential refresh lock is already held by this thread");
        }
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
                FileLock lock;
                try {
                    lock = channel.tryLock();
                } catch (OverlappingFileLockException heldInThisJvm) {
                    // Another channel of this JVM holds it; wait as for another process.
                    lock = null;
                }
                if (lock != null) {
                    final Held held = new Held(lockPath, channel, semaphore);
                    OWNERS.put(lockPath, Thread.currentThread());
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

    private static Path canonicalLockPath(final Path connectionsPath) {
        // Resolve links before ".." as the file system does, so a lexical normalize must come last.
        final Path absolute = connectionsPath.toAbsolutePath();
        final String lockName = absolute.getFileName().toString() + LOCK_FILE_SUFFIX;
        try {
            return absolute.getParent().toRealPath().resolve(lockName);
        } catch (IOException missingDirectory) {
            // A missing directory fails at open time; until then the normalized path names it.
            return absolute.normalize().resolveSibling(lockName);
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
        private final Path lockPath;
        private final FileChannel channel;
        private final Semaphore semaphore;
        private boolean isReleased;

        private Held(final Path lockPath, final FileChannel channel, final Semaphore semaphore) {
            this.lockPath = lockPath;
            this.channel = channel;
            this.semaphore = semaphore;
        }

        @Override
        public synchronized void close() {
            if (isReleased) {
                return;
            }
            isReleased = true;
            OWNERS.remove(lockPath);
            // Closing the channel releases its lock even when close reports an error.
            closeQuietly(channel);
            semaphore.release();
        }
    }
}
