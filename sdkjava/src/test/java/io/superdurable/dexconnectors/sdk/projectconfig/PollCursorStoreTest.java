// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;

import com.fasterxml.jackson.databind.JsonNode;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Base64;
import java.util.Collections;
import java.util.List;
import org.junit.jupiter.api.Test;

/** Replays the Go poll Trigger kit's cursor fixture that sdkpython and sdkts also replay. */
class PollCursorStoreTest {
    private static final Scope SCOPE = new Scope("sample-project", "preview");
    private static final PollCursorKey KEY = new PollCursorKey("sample", "main-é-流程 😀", "itemCreated", "main / west");
    private static final byte[] UNSEEDED = "{\"schemaVersion\":\"connectors.dex.dev/project-poll-cursor/v1alpha1\",\"state\":null}"
            .getBytes(StandardCharsets.UTF_8);
    private static final String HASH = "0123456789abcdef";

    @Test
    void headKeysMatchGo() throws IOException {
        for (JsonNode testCase : fixture().get("keys")) {
            final PollCursorIdentity identity = testCase.has("feedName")
                    ? new PollFeedKey(text(testCase, "connectorId"), text(testCase, "connectionName"),
                            text(testCase, "feedName"))
                    : new PollCursorKey(text(testCase, "connectorId"), text(testCase, "connectionName"),
                            text(testCase, "triggerName"), text(testCase, "bindingName"));
            assertEquals(text(testCase, "key"), identity.path(SCOPE));
            assertEquals(text(testCase, "key"), new PollCursorStore(new InMemoryObjectStore(), SCOPE, identity).getKey());
        }
    }

    @Test
    void documentsMatchGoBytes() throws IOException {
        for (JsonNode testCase : fixture().get("documents")) {
            final byte[] document = Base64.getDecoder().decode(text(testCase, "document"));
            assertArrayEquals(document, PollCursorDocuments.encode(state(testCase.get("state"))), text(testCase, "name"));
            assertArrayEquals(document, PollCursorDocuments.encode(PollCursorDocuments.decode(document)),
                    text(testCase, "name"));
        }
    }

    @Test
    void storedDocumentsDecodeAsGoDecodesThem() throws IOException {
        final String goldenHeadKey = text(fixture().get("keys").get(0), "key");
        for (JsonNode testCase : fixture().get("decoding")) {
            final InMemoryObjectStore objects = new InMemoryObjectStore();
            final byte[] input = Base64.getDecoder().decode(text(testCase, "input"));
            objects.createObject(goldenHeadKey, input);
            final PollCursorStore store = new PollCursorStore(objects, SCOPE, KEY);
            if (testCase.get("error").booleanValue()) {
                assertThrows(PollCursorDocumentInvalidException.class, store::readPollCursor, text(testCase, "name"));
                assertArrayEquals(input, objects.readObject(goldenHeadKey, "").getContents());
                continue;
            }
            assertArrayEquals(Base64.getDecoder().decode(text(testCase, "reencoded")),
                    PollCursorDocuments.encode(store.readPollCursor().getState()), text(testCase, "name"));
        }
    }

    @Test
    void createsTheHeadFirstAndWritesGoldenBytes() throws IOException {
        final JsonNode fixture = fixture();
        final List<String> createdKeys = new ArrayList<>();
        final int[] missingReads = {0};
        final InMemoryObjectStore delegate = new InMemoryObjectStore();
        final ObjectStore objects = new ObjectStore() {
            @Override
            public ProjectObject readObject(final String key, final String version) throws IOException {
                try {
                    return delegate.readObject(key, version);
                } catch (ObjectNotFoundException error) {
                    missingReads[0]++;
                    throw new IOException("access denied");
                }
            }

            @Override
            public ProjectObject createObject(final String key, final byte[] contents) throws IOException {
                createdKeys.add(key);
                return delegate.createObject(key, contents);
            }

            @Override
            public ProjectObject compareAndSwapObject(final String key, final String expectedEtag, final byte[] contents)
                    throws IOException {
                return delegate.compareAndSwapObject(key, expectedEtag, contents);
            }
        };
        final String goldenHeadKey = text(fixture.get("keys").get(0), "key");
        final PollCursorStore store = new PollCursorStore(objects, SCOPE, KEY);
        final PollCursorStore.PollCursorRecord record = store.readPollCursor();
        assertNull(record.getState());
        assertEquals(Collections.singletonList(goldenHeadKey), createdKeys);
        assertArrayEquals(UNSEEDED, delegate.readObject(goldenHeadKey, "").getContents());
        JsonNode golden = null;
        for (JsonNode testCase : fixture.get("documents")) {
            if ("golden".equals(text(testCase, "name"))) {
                golden = testCase;
            }
        }
        final PollCursorStore.PollCursorRecord written =
                store.replacePollCursor(record.getRevision(), state(golden.get("state")));
        assertArrayEquals(Base64.getDecoder().decode(text(golden, "document")),
                delegate.readObject(goldenHeadKey, "").getContents());
        final PollCursorStore.PollCursorRecord reread = store.readPollCursor();
        assertEquals(written.getRevision(), reread.getRevision());
        assertEquals("row <7> &  end", reread.getState().getToken());
        assertArrayEquals("{\"Name\":\"Ada\"}".getBytes(StandardCharsets.UTF_8),
                reread.getState().getSnapshot().getEntries().get(0).getValue());
        assertEquals(0, missingReads[0]);
        new PollCursorStore(objects, SCOPE, new PollFeedKey("sample", KEY.getConnectionName(), "itemChanges"))
                .readPollCursor();
        assertEquals(text(fixture.get("keys").get(3), "key"), createdKeys.get(createdKeys.size() - 1));
    }

    @Test
    void replicasShareOneHeadAndDetectConflicts() throws IOException {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final PollCursorStore first = new PollCursorStore(objects, SCOPE, KEY);
        final PollCursorStore second = new PollCursorStore(objects, SCOPE, KEY);
        final PollCursorStore.PollCursorRecord firstRead = first.readPollCursor();
        final PollCursorStore.PollCursorRecord secondRead = second.readPollCursor();
        assertEquals(firstRead.getRevision(), secondRead.getRevision());
        first.replacePollCursor(firstRead.getRevision(), PollCursorState.newBuilder().token("a").commitCount(1).build());
        assertThrows(ObjectConflictException.class, () -> second.replacePollCursor(secondRead.getRevision(),
                PollCursorState.newBuilder().token("b").commitCount(1).build()));
        assertEquals("a", second.readPollCursor().getState().getToken());
    }

    @Test
    void reconcilesUnknownWriteOutcomesByBytes() throws IOException {
        for (boolean shouldApply : new boolean[] {true, false}) {
            final InMemoryObjectStore delegate = new InMemoryObjectStore();
            final ObjectStore objects = new ObjectStore() {
                @Override
                public ProjectObject readObject(final String key, final String version) throws IOException {
                    return delegate.readObject(key, version);
                }

                @Override
                public ProjectObject createObject(final String key, final byte[] contents) throws IOException {
                    return delegate.createObject(key, contents);
                }

                @Override
                public ProjectObject compareAndSwapObject(final String key, final String expectedEtag,
                        final byte[] contents) throws IOException {
                    if (shouldApply) {
                        delegate.compareAndSwapObject(key, expectedEtag, contents);
                    }
                    throw new OutcomeUnknownException("write outcome unknown");
                }
            };
            final PollCursorStore store = new PollCursorStore(objects, SCOPE, KEY);
            final PollCursorStore.PollCursorRecord record = store.readPollCursor();
            final PollCursorState state = PollCursorState.newBuilder().token("x").commitCount(1).build();
            if (shouldApply) {
                assertEquals(store.replacePollCursor(record.getRevision(), state).getRevision(),
                        store.readPollCursor().getRevision());
            } else {
                assertThrows(ObjectConflictException.class, () -> store.replacePollCursor(record.getRevision(), state));
            }
        }
    }

    @Test
    void refusesOversizeAndMalformedStates() throws IOException {
        final PollCursorStore store = new PollCursorStore(new InMemoryObjectStore(), SCOPE, KEY);
        final String revision = store.readPollCursor().getRevision();
        final List<String> manyKeys = new ArrayList<>();
        for (int index = 0; index <= PollCursorDocuments.MAXIMUM_KEYED_SNAPSHOT_ENTRIES; index++) {
            manyKeys.add(String.format("k%06d", index));
        }
        final byte[] largeValue = new byte[(9 << 20) + 2];
        Arrays.fill(largeValue, (byte) 'v');
        largeValue[0] = '"';
        largeValue[largeValue.length - 1] = '"';
        final List<PollCursorState> tooLarge = Arrays.asList(
                PollCursorState.newBuilder().token(repeat("t", PollCursorDocuments.MAXIMUM_TOKEN_BYTES + 1)).build(),
                PollCursorState.newBuilder().token(repeat("é", PollCursorDocuments.MAXIMUM_TOKEN_BYTES / 2 + 1)).build(),
                PollCursorState.newBuilder().snapshot(entries(manyKeys.toArray(new String[0]))).build(),
                PollCursorState.newBuilder().snapshot(new KeyedSnapshot(Collections.singletonList(
                        new KeyedSnapshotEntry("a", HASH, 0, largeValue)))).build());
        for (PollCursorState state : tooLarge) {
            assertThrows(PollCursorTooLargeException.class, () -> store.replacePollCursor(revision, state));
        }
        final List<PollCursorState> malformed = Arrays.asList(
                PollCursorState.newBuilder().snapshot(entries("b", "a")).build(),
                PollCursorState.newBuilder().snapshot(entries("a", "a")).build(),
                PollCursorState.newBuilder().snapshot(entries("")).build(),
                PollCursorState.newBuilder().snapshot(single(new KeyedSnapshotEntry("a", "ABCDEF0123456789"))).build(),
                PollCursorState.newBuilder().snapshot(single(new KeyedSnapshotEntry("a", HASH, 0,
                        "null".getBytes(StandardCharsets.UTF_8)))).build(),
                PollCursorState.newBuilder().snapshot(single(new KeyedSnapshotEntry("a", HASH, 0,
                        "{".getBytes(StandardCharsets.UTF_8)))).build(),
                PollCursorState.newBuilder().snapshot(single(new KeyedSnapshotEntry("a", HASH, -1, null))).build(),
                PollCursorState.newBuilder().commitCount(-1).build(),
                PollCursorState.newBuilder().resetCount(-1).build());
        for (PollCursorState state : malformed) {
            final IllegalArgumentException error =
                    assertThrows(IllegalArgumentException.class, () -> store.replacePollCursor(revision, state));
            assertFalse(error instanceof PollCursorTooLargeException);
        }
        store.replacePollCursor(revision, PollCursorState.newBuilder()
                .snapshot(new KeyedSnapshot(Collections.<KeyedSnapshotEntry>emptyList())).build());
    }

    @Test
    void refusesInvalidIdentities() {
        for (PollCursorKey key : Arrays.asList(
                new PollCursorKey("Sample", "main", "itemCreated", "main"),
                new PollCursorKey("sample", " main", "itemCreated", "main"),
                new PollCursorKey("sample", "main", "item-created", "main"),
                new PollCursorKey("sample", "main", "itemCreated", "line\nbreak"),
                new PollCursorKey("sample", "main", "itemCreated", repeat("x", 257)))) {
            assertThrows(IllegalArgumentException.class, () -> new PollCursorStore(new InMemoryObjectStore(), SCOPE, key));
        }
        assertThrows(IllegalArgumentException.class, () -> new PollCursorStore(new InMemoryObjectStore(), SCOPE,
                new PollFeedKey("sample", "main", "Feed")));
        assertThrows(IllegalArgumentException.class, () -> new PollCursorStore(null, SCOPE, KEY));
    }

    private static JsonNode fixture() throws IOException {
        return MAPPER.readTree(Paths.get(System.getProperty("conformance.directory", "../conformance"),
                "..", "sdkpython", "tests", "fixtures", "go-poll-cursor-cases.json").toFile());
    }

    private static PollCursorState state(final JsonNode spec) {
        KeyedSnapshot snapshot = null;
        if (!spec.get("snapshot").isNull()) {
            final List<KeyedSnapshotEntry> entries = new ArrayList<>();
            for (JsonNode entry : spec.get("snapshot")) {
                entries.add(new KeyedSnapshotEntry(text(entry, "key"), text(entry, "hash"), entry.get("seen").longValue(),
                        entry.get("value").isNull() ? null : text(entry, "value").getBytes(StandardCharsets.UTF_8)));
            }
            snapshot = new KeyedSnapshot(entries);
        }
        return PollCursorState.newBuilder().token(text(spec, "token")).snapshot(snapshot)
                .seededAtUnixMs(spec.get("seededAtUnixMs").longValue())
                .committedAtUnixMs(spec.get("committedAtUnixMs").longValue())
                .commitCount(spec.get("commitCount").longValue()).resetCount(spec.get("resetCount").longValue())
                .lastResetAtUnixMs(spec.get("lastResetAtUnixMs").isNull() ? null
                        : spec.get("lastResetAtUnixMs").longValue())
                .build();
    }

    private static KeyedSnapshot entries(final String... keys) {
        final List<KeyedSnapshotEntry> entries = new ArrayList<>();
        for (String key : keys) {
            entries.add(new KeyedSnapshotEntry(key, HASH));
        }
        return new KeyedSnapshot(entries);
    }

    private static KeyedSnapshot single(final KeyedSnapshotEntry entry) {
        return new KeyedSnapshot(Collections.singletonList(entry));
    }

    private static String repeat(final String value, final int count) {
        final StringBuilder builder = new StringBuilder();
        for (int index = 0; index < count; index++) {
            builder.append(value);
        }
        return builder.toString();
    }

    private static String text(final JsonNode node, final String field) {
        return node.get(field).textValue();
    }
}
