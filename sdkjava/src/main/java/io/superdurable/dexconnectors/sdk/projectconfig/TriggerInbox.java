// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.function.UnaryOperator;

/**
 * Stores acknowledged Trigger events in project storage until the application consumes them.
 *
 * <p>A restarted or replaced replica delivers them in order. Writes use compare-and-swap against
 * concurrent replicas. The object key and document match the Go, Python, and TypeScript inboxes
 * byte for byte: events keep their stored raw JSON, the document is compact and HTML-safe, and
 * invalid UTF-8 in an event ID decodes to U+FFFD as Go decodes it. Instances are thread-safe.
 */
public final class TriggerInbox {
    /** Schema of one binding's inbox object, shared with the Go, Python, and TypeScript SDKs. */
    public static final String TRIGGER_INBOX_SCHEMA_VERSION = "connectors.dex.dev/project-trigger-inbox/v1alpha1";
    /** One binding's pending backlog bound; a source stops acknowledging new events beyond it. */
    public static final int MAXIMUM_PENDING_TRIGGER_EVENTS = 1000;
    /** One inbox object's size bound in bytes; S3 project storage still refuses objects over 4 MiB. */
    public static final int MAXIMUM_TRIGGER_INBOX_BYTES = 8 << 20;

    private static final int MAXIMUM_INBOX_WRITE_ATTEMPTS = 8;

    private final ObjectStore objects;
    private final TriggerInboxKey identity;
    private final String key;
    private volatile boolean hasHead;

    /**
     * Binds one binding's inbox in scope without I/O.
     *
     * @param objects the project object store
     * @param scope the project scope
     * @param identity the binding identity
     * @throws IllegalArgumentException if the store is missing or the identity is invalid
     */
    public TriggerInbox(final ObjectStore objects, final Scope scope, final TriggerInboxKey identity) {
        if (objects == null || scope == null || identity == null) {
            throw new IllegalArgumentException("trigger inbox object store, scope, and identity are required");
        }
        this.objects = objects;
        this.identity = identity;
        this.key = identity.path(scope);
    }

    /**
     * Returns the inbox head object key, for diagnostics and fixtures.
     *
     * @return the object key
     */
    public String getKey() {
        return key;
    }

    /**
     * Returns the binding identity this inbox belongs to.
     *
     * @return the identity
     */
    public TriggerInboxKey getIdentity() {
        return identity;
    }

    /**
     * Returns the pending events in acknowledgement order.
     *
     * @return the pending events
     * @throws IOException if storage fails
     * @throws IllegalArgumentException if the stored document is invalid
     */
    public List<PendingTriggerEvent> pending() throws IOException {
        return Collections.unmodifiableList(read().events);
    }

    /**
     * Appends an event once; an ID already pending is unchanged.
     *
     * @param event the event
     * @throws IllegalArgumentException for a blank ID or invalid raw JSON
     * @throws TriggerInboxFullException beyond 1000 events or 8 MiB
     * @throws IOException if storage fails or conflicts persist
     */
    public void add(final PendingTriggerEvent event) throws IOException {
        if (event == null || PollCursorNames.isGoBlank(event.getId()) || !isRawJson(event.eventBytes())) {
            throw new IllegalArgumentException("trigger inbox event identity or contents are invalid");
        }
        update(events -> {
            for (PendingTriggerEvent pending : events) {
                if (pending.getId().equals(event.getId())) {
                    return events;
                }
            }
            if (events.size() >= MAXIMUM_PENDING_TRIGGER_EVENTS) {
                throw new FullInbox();
            }
            final List<PendingTriggerEvent> changed = new ArrayList<>(events);
            changed.add(event);
            return changed;
        });
    }

    /**
     * Deletes a consumed event; removing an absent event succeeds.
     *
     * @param eventId the event ID
     * @throws IOException if storage fails or conflicts persist
     */
    public void remove(final String eventId) throws IOException {
        update(events -> {
            final List<PendingTriggerEvent> changed = new ArrayList<>();
            for (PendingTriggerEvent pending : events) {
                if (!pending.getId().equals(eventId)) {
                    changed.add(pending);
                }
            }
            return changed;
        });
    }

    private void update(final UnaryOperator<List<PendingTriggerEvent>> change) throws IOException {
        for (int attempt = 0; attempt < MAXIMUM_INBOX_WRITE_ATTEMPTS; attempt++) {
            final Head current = read();
            final List<PendingTriggerEvent> changed;
            try {
                changed = change.apply(current.events);
            } catch (FullInbox full) {
                throw new TriggerInboxFullException();
            }
            if (ids(changed).equals(ids(current.events))) {
                return;
            }
            final byte[] contents = encodeDocument(changed);
            if (contents.length > MAXIMUM_TRIGGER_INBOX_BYTES) {
                throw new TriggerInboxFullException();
            }
            try {
                if (current.etag.isEmpty()) {
                    objects.createObject(key, contents);
                } else {
                    objects.compareAndSwapObject(key, current.etag, contents);
                }
                return;
            } catch (OutcomeUnknownException unknown) {
                try {
                    if (Arrays.equals(objects.readObject(key, "").getContents(), contents)) {
                        return;
                    }
                } catch (IOException | RuntimeException unreadable) {
                    // An unreadable head is retried like a conflict.
                }
            } catch (ObjectConflictException conflict) {
                // Another replica changed the inbox; reread and reapply.
            }
        }
        throw new ObjectConflictException("project trigger inbox compare-and-swap conflict");
    }

    private Head read() throws IOException {
        createHead();
        final ProjectObject stored;
        try {
            stored = objects.readObject(key, "");
        } catch (ObjectNotFoundException missing) {
            return new Head(new ArrayList<PendingTriggerEvent>(), "");
        }
        if (stored.getEtag() == null || stored.getEtag().isEmpty()) {
            throw new IllegalArgumentException("trigger inbox document is invalid");
        }
        return new Head(decodeDocument(stored.getContents()), stored.getEtag());
    }

    // S3 refuses reading a missing key like a permission error, so the head is created first.
    private void createHead() throws IOException {
        if (hasHead) {
            return;
        }
        try {
            objects.createObject(key, encodeDocument(Collections.<PendingTriggerEvent>emptyList()));
            hasHead = true;
        } catch (ObjectConflictException exists) {
            hasHead = true;
        } catch (OutcomeUnknownException unknown) {
            // The next read decides whether the head exists.
        }
    }

    static byte[] encodeDocument(final List<PendingTriggerEvent> events) {
        final ByteArrayOutputStream output = new ByteArrayOutputStream();
        write(output, "{\"schemaVersion\":" + GoRawJson.encodeString(TRIGGER_INBOX_SCHEMA_VERSION) + ",\"events\":[");
        for (int index = 0; index < events.size(); index++) {
            final PendingTriggerEvent event = events.get(index);
            write(output, (index > 0 ? "," : "") + "{\"id\":" + GoRawJson.encodeString(event.getId()) + ",\"event\":");
            final byte[] compacted = GoRawJson.compact(event.eventBytes());
            output.write(compacted, 0, compacted.length);
            write(output, "}");
        }
        write(output, "]}");
        return output.toByteArray();
    }

    /** Decodes a document strictly, as Go's DisallowUnknownFields does; events keep raw bytes. */
    static List<PendingTriggerEvent> decodeDocument(final byte[] contents) {
        try {
            if (GoRawJson.kind(contents) != GoRawJson.Kind.OBJECT) {
                throw new GoRawJson.InvalidRawJsonException();
            }
            String schemaVersion = "";
            List<PendingTriggerEvent> events = new ArrayList<>();
            for (Map.Entry<String, byte[]> member : GoRawJson.objectMembers(contents)) {
                if (GoRawJson.isGoFieldKey(member.getKey(), "schemaVersion")) {
                    schemaVersion = GoRawJson.decodeStringField(member.getValue(), schemaVersion);
                    continue;
                }
                if (!GoRawJson.isGoFieldKey(member.getKey(), "events")) {
                    throw new GoRawJson.InvalidRawJsonException();
                }
                final GoRawJson.Kind kind = GoRawJson.kind(member.getValue());
                if (kind == GoRawJson.Kind.NULL) {
                    events = new ArrayList<>();
                    continue;
                }
                if (kind != GoRawJson.Kind.ARRAY) {
                    throw new GoRawJson.InvalidRawJsonException();
                }
                events = new ArrayList<>();
                for (byte[] element : GoRawJson.arrayElements(member.getValue())) {
                    events.add(decodePendingEvent(element));
                }
            }
            if (!TRIGGER_INBOX_SCHEMA_VERSION.equals(schemaVersion)) {
                throw new GoRawJson.InvalidRawJsonException();
            }
            return events;
        } catch (RuntimeException error) {
            throw new IllegalArgumentException("trigger inbox document is invalid", error);
        }
    }

    private static PendingTriggerEvent decodePendingEvent(final byte[] raw) {
        final GoRawJson.Kind kind = GoRawJson.kind(raw);
        if (kind == GoRawJson.Kind.NULL) {
            return new PendingTriggerEvent("", "null".getBytes(StandardCharsets.US_ASCII));
        }
        if (kind != GoRawJson.Kind.OBJECT) {
            throw new GoRawJson.InvalidRawJsonException();
        }
        String eventId = "";
        byte[] event = "null".getBytes(StandardCharsets.US_ASCII);
        for (Map.Entry<String, byte[]> member : GoRawJson.objectMembers(raw)) {
            if (GoRawJson.isGoFieldKey(member.getKey(), "id")) {
                eventId = GoRawJson.decodeStringField(member.getValue(), eventId);
            } else if (GoRawJson.isGoFieldKey(member.getKey(), "event")) {
                event = member.getValue();
            } else {
                throw new GoRawJson.InvalidRawJsonException();
            }
        }
        return new PendingTriggerEvent(eventId, event);
    }

    static boolean isRawJson(final byte[] value) {
        try {
            GoRawJson.compact(value);
            return true;
        } catch (RuntimeException error) {
            return false;
        }
    }

    private static List<String> ids(final List<PendingTriggerEvent> events) {
        final List<String> ids = new ArrayList<>(events.size());
        for (PendingTriggerEvent event : events) {
            ids.add(event.getId());
        }
        return ids;
    }

    private static void write(final ByteArrayOutputStream output, final String text) {
        final byte[] bytes = text.getBytes(StandardCharsets.UTF_8);
        output.write(bytes, 0, bytes.length);
    }

    private static final class Head {
        private final List<PendingTriggerEvent> events;
        private final String etag;

        private Head(final List<PendingTriggerEvent> events, final String etag) {
            this.events = events;
            this.etag = etag;
        }
    }

    private static final class FullInbox extends RuntimeException {
        private static final long serialVersionUID = 1L;
    }
}
