// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonValue;
import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.UUID;
import java.util.regex.Pattern;

/**
 * Stable UUID identity of one provider call across Dex Step retries.
 *
 * <p>{@link #derive} reproduces the Go length-prefixed SHA-1 UUID derivation byte for byte. An
 * empty value is allowed only as the zero Call ID of an uncorrelated {@link Receipt}.
 */
public final class CallId {
    /** The empty Call ID of an uncorrelated receipt. */
    public static final CallId EMPTY = new CallId("");

    /** The pinned UUIDv5 namespace of every derived Call ID. */
    public static final UUID NAMESPACE = nameBasedSha1(
            UUID.fromString("6ba7b811-9dad-11d1-80b4-00c04fd430c8"),
            "https://superdurable.dev/dex-connectors/call-id/v1".getBytes(StandardCharsets.UTF_8));

    private static final Pattern UUID_TEXT = Pattern.compile(
            "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$");

    private final String value;

    private CallId(final String value) {
        this.value = value;
    }

    /**
     * Wraps a Call ID without validating it; call {@link #validate()} before using it.
     *
     * @param value the Call ID text
     * @return the Call ID
     */
    @JsonCreator
    public static CallId of(final String value) {
        if (value == null) {
            throw new IllegalArgumentException("call ID is required");
        }
        return value.isEmpty() ? EMPTY : new CallId(value);
    }

    /**
     * Derives occurrence 1, the first call of an operation in one Dex Step attempt.
     *
     * @param flowId the Dex Flow ID
     * @param stepExecutionId the Dex Step execution ID
     * @param operation the operation identity
     * @return the lowercase UUIDv5 Call ID
     * @throws IllegalArgumentException if an ID is empty or the operation is invalid
     */
    public static CallId derive(final String flowId, final String stepExecutionId, final OperationRef operation) {
        return derive(flowId, stepExecutionId, operation, 1);
    }

    /**
     * Derives the n-th call of an operation in one Dex Step attempt.
     *
     * <p>The payload concatenates the Flow ID, Step execution ID, connector ID, and operation ID,
     * each UTF-8 encoded and preceded by its four-byte big-endian byte length. From occurrence 2,
     * the decimal occurrence is a fifth field.
     *
     * @param flowId the Dex Flow ID
     * @param stepExecutionId the Dex Step execution ID
     * @param operation the operation identity
     * @param occurrence the one-based ordinal of this call among the same operation's calls
     * @return the lowercase UUIDv5 Call ID
     * @throws IllegalArgumentException if an ID is empty, the operation is invalid, or the
     *     occurrence is not positive
     */
    public static CallId derive(
            final String flowId, final String stepExecutionId, final OperationRef operation, final int occurrence) {
        if (flowId == null || flowId.isEmpty() || stepExecutionId == null || stepExecutionId.isEmpty()) {
            throw new IllegalArgumentException("Dex Flow and Step execution IDs are required");
        }
        if (occurrence < 1) {
            throw new IllegalArgumentException("call occurrence must be a positive integer");
        }
        operation.validate();
        final ByteArrayOutputStream payload = new ByteArrayOutputStream();
        appendLengthPrefixed(payload, flowId);
        appendLengthPrefixed(payload, stepExecutionId);
        appendLengthPrefixed(payload, operation.getConnectorId());
        appendLengthPrefixed(payload, operation.getOperationId());
        if (occurrence > 1) {
            appendLengthPrefixed(payload, Integer.toString(occurrence));
        }
        return new CallId(nameBasedSha1(NAMESPACE, payload.toByteArray()).toString());
    }

    /**
     * Requires a UUID value.
     *
     * @throws IllegalArgumentException if the value is not a UUID
     */
    public void validate() {
        if (!UUID_TEXT.matcher(value).matches()) {
            throw new IllegalArgumentException("call ID must be a UUID");
        }
    }

    /**
     * Reports whether this is the empty Call ID.
     *
     * @return {@code true} for {@link #EMPTY}
     */
    public boolean isEmpty() {
        return value.isEmpty();
    }

    /**
     * Returns the Call ID text.
     *
     * @return the UUID text, or an empty string
     */
    @JsonValue
    public String value() {
        return value;
    }

    @Override
    public String toString() {
        return value;
    }

    @Override
    public boolean equals(final Object other) {
        return other instanceof CallId && value.equals(((CallId) other).value);
    }

    @Override
    public int hashCode() {
        return value.hashCode();
    }

    private static void appendLengthPrefixed(final ByteArrayOutputStream payload, final String field) {
        final byte[] bytes = field.getBytes(StandardCharsets.UTF_8);
        payload.write(ByteBuffer.allocate(4).putInt(bytes.length).array(), 0, 4);
        payload.write(bytes, 0, bytes.length);
    }

    private static UUID nameBasedSha1(final UUID namespace, final byte[] name) {
        final MessageDigest digest;
        try {
            digest = MessageDigest.getInstance("SHA-1");
        } catch (NoSuchAlgorithmException error) {
            throw new IllegalStateException("SHA-1 is required by every Java platform", error);
        }
        final ByteBuffer namespaceBytes = ByteBuffer.allocate(16);
        namespaceBytes.putLong(namespace.getMostSignificantBits());
        namespaceBytes.putLong(namespace.getLeastSignificantBits());
        digest.update(namespaceBytes.array());
        digest.update(name);
        final byte[] hash = digest.digest();
        hash[6] = (byte) ((hash[6] & 0x0f) | 0x50);
        hash[8] = (byte) ((hash[8] & 0x3f) | 0x80);
        final ByteBuffer uuidBytes = ByteBuffer.wrap(hash, 0, 16);
        final long mostSignificantBits = uuidBytes.getLong();
        return new UUID(mostSignificantBits, uuidBytes.getLong());
    }
}
