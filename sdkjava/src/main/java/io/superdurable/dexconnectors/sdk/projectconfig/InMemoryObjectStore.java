// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Deterministic versioned object store for local Dex Web projects and tests.
 *
 * <p>Versions count from 1 per key, and each ETag is the lowercase hexadecimal SHA-256 of the
 * contents followed by {@code -<version>}, as the Go local store writes it. Thread-safe.
 */
public final class InMemoryObjectStore implements ObjectStore {
    /** Largest ordinary object body in bytes. */
    public static final int MAXIMUM_OBJECT_BYTES = 4 << 20;
    /** Largest local Trigger inbox or poll cursor body in bytes. */
    public static final int MAXIMUM_LOCAL_TRIGGER_STATE_BYTES = 8 << 20;

    private final Map<String, List<ProjectObject>> objects = new HashMap<>();

    /**
     * Creates an empty store.
     */
    public InMemoryObjectStore() {
    }

    /** {@inheritDoc} */
    @Override
    public synchronized ProjectObject readObject(final String key, final String version) throws ObjectNotFoundException {
        validateObjectKey(key);
        final List<ProjectObject> versions = objects.get(key);
        if (versions != null) {
            for (int index = versions.size() - 1; index >= 0; index--) {
                final ProjectObject item = versions.get(index);
                if (version == null || version.isEmpty() || item.getVersion().equals(version)) {
                    return item;
                }
            }
        }
        throw new ObjectNotFoundException("project object not found");
    }

    /** {@inheritDoc} */
    @Override
    public synchronized ProjectObject createObject(final String key, final byte[] contents)
            throws ObjectConflictException {
        validateObjectKey(key);
        validateContents(key, contents);
        if (objects.containsKey(key)) {
            throw new ObjectConflictException("project object already exists");
        }
        return append(key, contents);
    }

    /** {@inheritDoc} */
    @Override
    public synchronized ProjectObject compareAndSwapObject(
            final String key, final String expectedEtag, final byte[] contents) throws ObjectConflictException {
        validateObjectKey(key);
        validateContents(key, contents);
        final List<ProjectObject> versions = objects.get(key);
        if (versions == null || versions.isEmpty() || expectedEtag == null || expectedEtag.isEmpty()
                || !versions.get(versions.size() - 1).getEtag().equals(expectedEtag)) {
            throw new ObjectConflictException("project object compare-and-swap conflict");
        }
        return append(key, contents);
    }

    /**
     * Rejects traversal, separators, controls, and oversized object identities.
     *
     * @param key the object key
     * @throws IllegalArgumentException if the key is invalid
     */
    public static void validateObjectKey(final String key) {
        if (key == null || key.isEmpty() || key.length() > 1024 || key.startsWith("/")
                || key.indexOf('\\') >= 0 || key.indexOf('\u0000') >= 0 || key.indexOf('\r') >= 0
                || key.indexOf('\n') >= 0) {
            throw new IllegalArgumentException("invalid project object key");
        }
        for (String segment : key.split("/", -1)) {
            if (segment.isEmpty() || ".".equals(segment) || "..".equals(segment)) {
                throw new IllegalArgumentException("invalid project object key");
            }
        }
    }

    static int localObjectByteLimit(final String key) {
        final boolean isTriggerState = key.contains("/trigger-inboxes/") || key.contains("/poll-cursors/")
                || key.contains("/poll-feeds/");
        return isTriggerState ? MAXIMUM_LOCAL_TRIGGER_STATE_BYTES : MAXIMUM_OBJECT_BYTES;
    }

    private static void validateContents(final String key, final byte[] contents) {
        if (contents == null || contents.length == 0 || contents.length > localObjectByteLimit(key)) {
            throw new IllegalArgumentException("project object body is empty or oversized");
        }
    }

    private ProjectObject append(final String key, final byte[] contents) {
        final List<ProjectObject> versions = objects.computeIfAbsent(key, ignored -> new ArrayList<>());
        final String version = Integer.toString(versions.size() + 1);
        final ProjectObject item = new ProjectObject(key, version,
                GoCompatibleJson.sha256Hex(contents) + "-" + version, contents);
        versions.add(item);
        return item;
    }
}
