// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * Exact-version reads and single-attempt conditional writes of private project objects.
 *
 * <p>Implementations are thread-safe. A write that may have reached storage without a known
 * result throws {@link OutcomeUnknownException}; callers reconcile it by reading back.
 */
public interface ObjectStore {
    /**
     * Reads the exact version, or the current head when the version is empty.
     *
     * @param key the object key
     * @param version the exact version, or empty for the head
     * @return the object
     * @throws ObjectNotFoundException if the key or version does not exist
     * @throws IOException if storage fails
     */
    ProjectObject readObject(String key, String version) throws IOException;

    /**
     * Creates the object only if the key is absent.
     *
     * @param key the object key
     * @param contents the bytes
     * @return the written object
     * @throws ObjectConflictException if the key exists
     * @throws OutcomeUnknownException if the outcome is unknown
     * @throws IOException if storage fails
     */
    ProjectObject createObject(String key, byte[] contents) throws IOException;

    /**
     * Replaces the object only when its current ETag equals the expected one.
     *
     * @param key the object key
     * @param expectedEtag the exact ETag that was read
     * @param contents the bytes
     * @return the written object
     * @throws ObjectConflictException if another writer changed the object
     * @throws OutcomeUnknownException if the outcome is unknown
     * @throws IOException if storage fails
     */
    ProjectObject compareAndSwapObject(String key, String expectedEtag, byte[] contents) throws IOException;
}
