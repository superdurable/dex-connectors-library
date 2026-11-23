// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.ByteArrayOutputStream;
import java.math.BigInteger;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Consumer;
import java.util.regex.Pattern;

/**
 * Encodes and strictly decodes poll cursor documents byte-compatibly with the Go Connector SDK.
 */
public final class PollCursorDocuments {
    /** Schema of one poll cursor head object. */
    public static final String SCHEMA_VERSION = "connectors.dex.dev/project-poll-cursor/v1alpha1";
    /** Bound of {@link PollCursorState#getToken()} in UTF-8 bytes. */
    public static final int MAXIMUM_TOKEN_BYTES = 64 << 10;
    /** Hard bound on the entries of one stored keyed snapshot. */
    public static final int MAXIMUM_KEYED_SNAPSHOT_ENTRIES = 100_000;
    /** Bound of one encoded document in bytes, 8 MiB as in Go; S3 storage refuses over 4 MiB. */
    public static final int MAXIMUM_DOCUMENT_BYTES = 8 << 20;

    private static final int MAXIMUM_KEY_BYTES = 1 << 10;
    private static final Pattern HASH = Pattern.compile("^[0-9a-f]{16,64}$");
    private static final Pattern INT64_LITERAL = Pattern.compile("^-?[0-9]+$");
    private static final BigInteger MINIMUM_INT64 = BigInteger.valueOf(Long.MIN_VALUE);
    private static final BigInteger MAXIMUM_INT64 = BigInteger.valueOf(Long.MAX_VALUE);

    private PollCursorDocuments() {
    }

    /**
     * Checks a state's storage bounds and snapshot ordering.
     *
     * @param state the state
     * @throws PollCursorTooLargeException for an oversize token or snapshot
     * @throws IllegalArgumentException for an unsorted or duplicate key, a malformed hash, a null
     *     value, or a negative counter
     */
    public static void validateState(final PollCursorState state) {
        final int tokenBytes = GoCompatibleJson.wellFormed(state.getToken()).getBytes(StandardCharsets.UTF_8).length;
        if (tokenBytes > MAXIMUM_TOKEN_BYTES) {
            throw new PollCursorTooLargeException(
                    "project poll cursor state exceeds its bound: token is " + tokenBytes + " bytes");
        }
        if (state.getCommitCount() < 0 || state.getResetCount() < 0) {
            throw new IllegalArgumentException("poll cursor counters must not be negative");
        }
        if (state.getSnapshot() != null) {
            validateSnapshot(state.getSnapshot());
        }
    }

    /**
     * Checks that entries are sorted by unique key with valid hashes and values.
     *
     * @param snapshot the snapshot
     * @throws PollCursorTooLargeException over the entry bound
     * @throws IllegalArgumentException for any other violation
     */
    public static void validateSnapshot(final KeyedSnapshot snapshot) {
        final List<KeyedSnapshotEntry> entries = snapshot.getEntries();
        if (entries.size() > MAXIMUM_KEYED_SNAPSHOT_ENTRIES) {
            throw new PollCursorTooLargeException(
                    "project poll cursor state exceeds its bound: snapshot has " + entries.size() + " entries");
        }
        String previousKey = null;
        for (KeyedSnapshotEntry entry : entries) {
            final int keyBytes = GoCompatibleJson.wellFormed(entry.getKey()).getBytes(StandardCharsets.UTF_8).length;
            if (keyBytes == 0 || keyBytes > MAXIMUM_KEY_BYTES) {
                throw new IllegalArgumentException("keyed snapshot key must be 1 byte to 1 KiB");
            }
            if (previousKey != null && GoCompatibleJson.compareUtf8(previousKey, entry.getKey()) >= 0) {
                throw new IllegalArgumentException("keyed snapshot entries must be sorted by unique key");
            }
            previousKey = entry.getKey();
            if (!HASH.matcher(entry.getHash()).matches()) {
                throw new IllegalArgumentException("keyed snapshot hash must be lowercase hex of 16 to 64 characters");
            }
            if (entry.getSeen() < 0) {
                throw new IllegalArgumentException("keyed snapshot seen counter must not be negative");
            }
            if (entry.getValue() != null && !isJsonOtherThanNull(entry.getValue())) {
                throw new IllegalArgumentException("keyed snapshot value must be JSON other than null");
            }
        }
    }

    /**
     * Encodes a document exactly as Go marshals it.
     *
     * @param state the state, or {@code null} for the unseeded head
     * @return the document bytes
     * @throws PollCursorTooLargeException over a bound, including a document over 8 MiB
     * @throws IllegalArgumentException for another invalid state
     */
    public static byte[] encode(final PollCursorState state) {
        final ByteArrayOutputStream output = new ByteArrayOutputStream();
        write(output, "{\"schemaVersion\":" + GoRawJson.encodeString(SCHEMA_VERSION) + ",\"state\":");
        if (state == null) {
            write(output, "null}");
            return output.toByteArray();
        }
        validateState(state);
        write(output, "{\"token\":" + GoRawJson.encodeString(state.getToken()) + ",\"snapshot\":");
        if (state.getSnapshot() == null) {
            write(output, "null");
        } else {
            write(output, "{\"entries\":[");
            int index = 0;
            for (KeyedSnapshotEntry entry : state.getSnapshot().getEntries()) {
                write(output, (index++ > 0 ? "," : "") + "{\"key\":" + GoRawJson.encodeString(entry.getKey())
                        + ",\"hash\":" + GoRawJson.encodeString(entry.getHash()) + ",\"seen\":" + entry.getSeen());
                if (entry.getValue() != null) {
                    write(output, ",\"value\":");
                    final byte[] compacted = GoRawJson.compact(entry.getValue());
                    output.write(compacted, 0, compacted.length);
                }
                write(output, "}");
            }
            write(output, "]}");
        }
        write(output, ",\"seededAtUnixMs\":" + state.getSeededAtUnixMs() + ",\"committedAtUnixMs\":"
                + state.getCommittedAtUnixMs() + ",\"commitCount\":" + state.getCommitCount() + ",\"resetCount\":"
                + state.getResetCount() + ",\"lastResetAtUnixMs\":"
                + (state.getLastResetAtUnixMs() == null ? "null" : state.getLastResetAtUnixMs().toString()) + "}}");
        final byte[] contents = output.toByteArray();
        if (contents.length > MAXIMUM_DOCUMENT_BYTES) {
            throw new PollCursorTooLargeException(
                    "project poll cursor state exceeds its bound: encoded document is " + contents.length + " bytes");
        }
        return contents;
    }

    /**
     * Decodes a document strictly, as Go's decoder with {@code DisallowUnknownFields} does.
     *
     * <p>Keys match case-insensitively, the last duplicate wins, a duplicate object merges into the
     * earlier one, null leaves a field unchanged, and invalid UTF-8 in strings becomes U+FFFD.
     *
     * @param contents the stored bytes
     * @return the state, or {@code null} for the unseeded head
     * @throws PollCursorDocumentInvalidException for any other schema, unknown field, malformed
     *     value, trailing data, or a state that fails {@link #validateState}
     */
    public static PollCursorState decode(final byte[] contents) {
        DecodedState state = null;
        try {
            if (GoRawJson.kind(contents) != GoRawJson.Kind.OBJECT) {
                throw new GoRawJson.InvalidRawJsonException();
            }
            String schemaVersion = "";
            for (Map.Entry<String, byte[]> member : GoRawJson.objectMembers(contents)) {
                if (GoRawJson.isGoFieldKey(member.getKey(), "schemaVersion")) {
                    schemaVersion = GoRawJson.decodeStringField(member.getValue(), schemaVersion);
                } else if (GoRawJson.isGoFieldKey(member.getKey(), "state")) {
                    state = decodeState(member.getValue(), state);
                } else {
                    throw new GoRawJson.InvalidRawJsonException();
                }
            }
            if (!SCHEMA_VERSION.equals(schemaVersion)) {
                throw new GoRawJson.InvalidRawJsonException();
            }
        } catch (IllegalArgumentException | IndexOutOfBoundsException error) {
            throw new PollCursorDocumentInvalidException("project poll cursor document is invalid");
        }
        if (state == null) {
            return null;
        }
        final PollCursorState decoded = state.freeze();
        try {
            validateState(decoded);
        } catch (IllegalArgumentException error) {
            throw new PollCursorDocumentInvalidException("project poll cursor document is invalid: " + error.getMessage());
        }
        return decoded;
    }

    private static boolean isJsonOtherThanNull(final byte[] value) {
        try {
            return !"null".equals(new String(GoRawJson.compact(value), StandardCharsets.UTF_8));
        } catch (IllegalArgumentException error) {
            return false;
        }
    }

    private static void write(final ByteArrayOutputStream output, final String text) {
        final byte[] bytes = text.getBytes(StandardCharsets.UTF_8);
        output.write(bytes, 0, bytes.length);
    }

    private static boolean decodeStructFields(final byte[] raw, final Map<String, Consumer<byte[]>> fields) {
        final GoRawJson.Kind kind = GoRawJson.kind(raw);
        if (kind == GoRawJson.Kind.NULL) {
            return false;
        }
        if (kind != GoRawJson.Kind.OBJECT) {
            throw new GoRawJson.InvalidRawJsonException();
        }
        for (Map.Entry<String, byte[]> member : GoRawJson.objectMembers(raw)) {
            Consumer<byte[]> handler = fields.get(member.getKey());
            if (handler == null) {
                for (Map.Entry<String, Consumer<byte[]>> candidate : fields.entrySet()) {
                    if (GoRawJson.isGoFieldKey(member.getKey(), candidate.getKey())) {
                        handler = candidate.getValue();
                        break;
                    }
                }
            }
            if (handler == null) {
                throw new GoRawJson.InvalidRawJsonException();
            }
            handler.accept(member.getValue());
        }
        return true;
    }

    private static long decodeInt64(final byte[] raw, final long current) {
        final GoRawJson.Kind kind = GoRawJson.kind(raw);
        if (kind == GoRawJson.Kind.NULL) {
            return current;
        }
        final String literal = new String(GoRawJson.trimLeadingWhitespace(raw), StandardCharsets.US_ASCII);
        if (kind != GoRawJson.Kind.NUMBER || !INT64_LITERAL.matcher(literal).matches()) {
            throw new GoRawJson.InvalidRawJsonException();
        }
        final BigInteger value = new BigInteger(literal);
        if (value.compareTo(MINIMUM_INT64) < 0 || value.compareTo(MAXIMUM_INT64) > 0) {
            throw new GoRawJson.InvalidRawJsonException();
        }
        return value.longValue();
    }

    private static DecodedState decodeState(final byte[] raw, final DecodedState current) {
        if (GoRawJson.kind(raw) == GoRawJson.Kind.NULL) {
            return null;
        }
        final DecodedState state = current != null ? current : new DecodedState();
        final Map<String, Consumer<byte[]>> fields = new LinkedHashMap<>();
        fields.put("token", value -> state.token = GoRawJson.decodeStringField(value, state.token));
        fields.put("snapshot", value -> state.snapshot = decodeSnapshot(value, state.snapshot));
        fields.put("seededAtUnixMs", value -> state.seededAtUnixMs = decodeInt64(value, state.seededAtUnixMs));
        fields.put("committedAtUnixMs", value -> state.committedAtUnixMs = decodeInt64(value, state.committedAtUnixMs));
        fields.put("commitCount", value -> state.commitCount = decodeInt64(value, state.commitCount));
        fields.put("resetCount", value -> state.resetCount = decodeInt64(value, state.resetCount));
        fields.put("lastResetAtUnixMs", value -> state.lastResetAtUnixMs =
                GoRawJson.kind(value) == GoRawJson.Kind.NULL ? null : decodeInt64(value, 0));
        decodeStructFields(raw, fields);
        return state;
    }

    private static DecodedSnapshot decodeSnapshot(final byte[] raw, final DecodedSnapshot current) {
        if (GoRawJson.kind(raw) == GoRawJson.Kind.NULL) {
            return null;
        }
        final DecodedSnapshot snapshot = current != null ? current : new DecodedSnapshot();
        final Map<String, Consumer<byte[]>> fields = new LinkedHashMap<>();
        fields.put("entries", value -> {
            final GoRawJson.Kind kind = GoRawJson.kind(value);
            if (kind == GoRawJson.Kind.NULL) {
                snapshot.entries = null;
                snapshot.backing = new ArrayList<>();
                return;
            }
            if (kind != GoRawJson.Kind.ARRAY) {
                throw new GoRawJson.InvalidRawJsonException();
            }
            final List<byte[]> elements = GoRawJson.arrayElements(value);
            // Go reuses a slice's earlier elements when a duplicate key decodes another array into it.
            while (snapshot.backing.size() < elements.size()) {
                snapshot.backing.add(new DecodedEntry());
            }
            for (int index = 0; index < elements.size(); index++) {
                decodeEntry(elements.get(index), snapshot.backing.get(index));
            }
            snapshot.entries = new ArrayList<>(snapshot.backing.subList(0, elements.size()));
        });
        decodeStructFields(raw, fields);
        return snapshot;
    }

    private static void decodeEntry(final byte[] raw, final DecodedEntry entry) {
        final Map<String, Consumer<byte[]>> fields = new LinkedHashMap<>();
        fields.put("key", value -> entry.key = GoRawJson.decodeStringField(value, entry.key));
        fields.put("hash", value -> entry.hash = GoRawJson.decodeStringField(value, entry.hash));
        fields.put("seen", value -> entry.seen = decodeInt64(value, entry.seen));
        fields.put("value", value -> entry.value = value.clone());
        decodeStructFields(raw, fields);
    }

    private static final class DecodedEntry {
        private String key = "";
        private String hash = "";
        private long seen;
        private byte[] value;
    }

    private static final class DecodedSnapshot {
        private List<DecodedEntry> entries;
        private List<DecodedEntry> backing = new ArrayList<>();
    }

    private static final class DecodedState {
        private String token = "";
        private DecodedSnapshot snapshot;
        private long seededAtUnixMs;
        private long committedAtUnixMs;
        private long commitCount;
        private long resetCount;
        private Long lastResetAtUnixMs;

        private PollCursorState freeze() {
            KeyedSnapshot frozenSnapshot = null;
            if (snapshot != null) {
                final List<KeyedSnapshotEntry> entries = new ArrayList<>();
                if (snapshot.entries != null) {
                    for (DecodedEntry entry : snapshot.entries) {
                        entries.add(new KeyedSnapshotEntry(entry.key, entry.hash, entry.seen, entry.value));
                    }
                }
                frozenSnapshot = new KeyedSnapshot(entries);
            }
            return PollCursorState.newBuilder().token(token).snapshot(frozenSnapshot).seededAtUnixMs(seededAtUnixMs)
                    .committedAtUnixMs(committedAtUnixMs).commitCount(commitCount).resetCount(resetCount)
                    .lastResetAtUnixMs(lastResetAtUnixMs).build();
        }
    }
}
