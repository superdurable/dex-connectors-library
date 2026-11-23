// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.IOException;

/**
 * Reads and conditionally replaces one poll cursor head in project storage, beside the Trigger
 * inboxes.
 *
 * <p>Concurrent replicas are ordered by compare-and-swap. The object key and document bytes match
 * the Go Connector SDK, so Go, Python, TypeScript, and Java replicas can share one cursor.
 */
public final class PollCursorStore {
    private final ObjectStore objects;
    private final String key;
    private volatile boolean hasHead;

    /**
     * Binds one binding's or feed's cursor in scope without I/O.
     *
     * @param objects the project object store
     * @param scope the project scope
     * @param identity the cursor identity
     * @throws IllegalArgumentException for a missing store or an invalid identity
     */
    public PollCursorStore(final ObjectStore objects, final Scope scope, final PollCursorIdentity identity) {
        if (objects == null) {
            throw new IllegalArgumentException("poll cursor object store is required");
        }
        this.objects = objects;
        this.key = identity.path(scope);
    }

    /**
     * Returns the cursor head object key, for diagnostics and fixtures.
     *
     * @return the key
     */
    public String getKey() {
        return key;
    }

    /**
     * Returns the current record; its state is {@code null} until the cursor is seeded.
     *
     * <p>Before the first read the store creates the unseeded head with a create-only write, so it
     * never reads a missing key, which S3 refuses like a permission error.
     *
     * @return the record
     * @throws PollCursorDocumentInvalidException if the stored document fails strict decoding
     * @throws IOException if storage fails
     */
    public PollCursorRecord readPollCursor() throws IOException {
        createHead();
        final ProjectObject stored;
        try {
            stored = objects.readObject(key, "");
        } catch (ObjectNotFoundException error) {
            return new PollCursorRecord(null, "");
        }
        final PollCursorState state = PollCursorDocuments.decode(stored.getContents());
        if (stored.getEtag().isEmpty()) {
            throw new PollCursorDocumentInvalidException("project poll cursor document is invalid: missing revision");
        }
        hasHead = true;
        return new PollCursorRecord(state, stored.getEtag());
    }

    /**
     * Writes the state only while the head is still at the expected revision.
     *
     * <p>An unknown write outcome is reconciled by reading the head back and comparing bytes; a
     * mismatch is a conflict. It never retries with another precondition: the caller re-reads and
     * decides again.
     *
     * @param expectedRevision the revision from the last read, or empty to create the head
     * @param state the new state
     * @return the written record
     * @throws ObjectConflictException if another writer changed the head first
     * @throws PollCursorTooLargeException for a state over a storage bound
     * @throws IOException if storage fails or the outcome stays unknown
     */
    public PollCursorRecord replacePollCursor(final String expectedRevision, final PollCursorState state)
            throws IOException {
        final byte[] contents = PollCursorDocuments.encode(state);
        ProjectObject written;
        try {
            written = expectedRevision == null || expectedRevision.isEmpty()
                    ? objects.createObject(key, contents)
                    : objects.compareAndSwapObject(key, expectedRevision, contents);
        } catch (OutcomeUnknownException unknownOutcome) {
            final ProjectObject observed;
            try {
                observed = objects.readObject(key, "");
            } catch (IOException | RuntimeException error) {
                // An unreadable head leaves the outcome unknown, as Go reports it.
                throw unknownOutcome;
            }
            if (!observed.hasContents(contents)) {
                throw new ObjectConflictException("project poll cursor compare-and-swap conflict");
            }
            written = observed;
        }
        hasHead = true;
        return new PollCursorRecord(state, written.getEtag());
    }

    private void createHead() throws IOException {
        if (hasHead) {
            return;
        }
        try {
            objects.createObject(key, PollCursorDocuments.encode(null));
            hasHead = true;
        } catch (ObjectConflictException error) {
            hasHead = true;
        } catch (OutcomeUnknownException error) {
            // The next read decides whether the create landed.
        }
    }

    /**
     * One read of a poll cursor head.
     */
    public static final class PollCursorRecord {
        private final PollCursorState state;
        private final String revision;

        PollCursorRecord(final PollCursorState state, final String revision) {
            this.state = state;
            this.revision = revision;
        }

        /**
         * Returns the committed position.
         *
         * @return the state, or {@code null} until the cursor is seeded
         */
        public PollCursorState getState() {
            return state;
        }

        /**
         * Returns the opaque compare-and-swap token for {@link #replacePollCursor}.
         *
         * @return the revision
         */
        public String getRevision() {
            return revision;
        }
    }
}
