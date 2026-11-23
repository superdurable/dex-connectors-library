// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import io.superdurable.dexconnectors.sdk.trigger.TriggerEvent;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.OffsetDateTime;
import java.util.Map;

/**
 * Encodes and decodes Trigger events stored in a {@link TriggerInbox}, byte-compatible with Go.
 */
public final class TriggerEventCodec {
    private static final ObjectMapper PAYLOAD_MAPPER = new ObjectMapper().findAndRegisterModules();

    private TriggerEventCodec() {
    }

    /**
     * Decodes a replayed payload.
     *
     * @param <P> the payload type
     */
    public interface PayloadDecoder<P> {
        /**
         * Decodes one payload.
         *
         * @param payloadJson the payload JSON text, with invalid UTF-8 replaced as Go does
         * @param payloadBytes a private copy of the stored payload bytes
         * @return the payload
         * @throws Exception when the payload cannot be decoded
         */
        P decode(String payloadJson, byte[] payloadBytes) throws Exception;
    }

    /**
     * Maps a payload to the JSON value stored for it.
     *
     * @param <P> the payload type
     */
    public interface PayloadEncoder<P> {
        /**
         * Returns the stored JSON value; objects are written with sorted keys as Go writes maps.
         *
         * @param payload the payload
         * @return the JSON value
         * @throws Exception when the payload cannot be encoded
         */
        JsonNode encode(P payload) throws Exception;
    }

    /**
     * Returns the default replay decoder, which parses the payload as a Jackson tree.
     *
     * @return the decoder
     */
    public static PayloadDecoder<JsonNode> jsonPayloadDecoder() {
        return (payloadJson, payloadBytes) -> PAYLOAD_MAPPER.readTree(payloadJson);
    }

    /**
     * Encodes an event in Go's form {@code {"eventId","occurredAt","payload"}}.
     *
     * <p>The time keeps its offset in Go's RFC 3339 Nano format, as Go's {@code time.Time} writes
     * it; the Python SDK converts it to UTC, so the bytes differ for non-UTC times while the instant
     * is the same. An offset that is not a whole number of minutes, such as the historical
     * {@code +00:09:21}, is written in UTC with the same instant. Without an encoder, the payload
     * is converted with Jackson. Object keys are sorted by their UTF-8 bytes, and the output is
     * compact and HTML-safe, as Go's {@code encoding/json} writes the equivalent map.
     *
     * @param event the event
     * @param encodePayload the payload encoder, or {@code null} to convert the payload with Jackson
     * @param <P> the payload type
     * @return the encoded event bytes
     * @throws Exception when the payload encoder fails or the payload is not JSON-encodable
     */
    public static <P> byte[] encodeTriggerEvent(final TriggerEvent<P> event, final PayloadEncoder<P> encodePayload)
            throws Exception {
        final JsonNode payload = encodePayload != null ? encodePayload.encode(event.getPayload())
                : PAYLOAD_MAPPER.valueToTree(event.getPayload());
        final String prefix = "{\"eventId\":" + GoRawJson.encodeString(event.getId()) + ",\"occurredAt\":"
                + GoRawJson.encodeString(occurredAtWire(event.getOccurredAt()))
                + ",\"payload\":";
        final ByteArrayOutputStream output = new ByteArrayOutputStream();
        final byte[] prefixBytes = prefix.getBytes(StandardCharsets.UTF_8);
        output.write(prefixBytes, 0, prefixBytes.length);
        final byte[] payloadBytes = GoCompatibleJson.encodeSortedMap(payload);
        output.write(payloadBytes, 0, payloadBytes.length);
        output.write('}');
        return output.toByteArray();
    }

    // RFC 3339 offsets are whole minutes; a historical second-level offset is written in UTC, keeping the instant.
    private static String occurredAtWire(final OffsetDateTime occurredAt) {
        if (occurredAt.getOffset().getTotalSeconds() % 60 != 0) {
            return Rfc3339NanoTimestamp.fromInstant(occurredAt.toInstant()).toWire();
        }
        return Rfc3339NanoTimestamp.fromOffsetDateTime(occurredAt).toWire();
    }

    /**
     * Decodes an event that {@link #encodeTriggerEvent} or Go wrote, as Go's {@code json.Unmarshal}.
     *
     * <p>Unknown fields are ignored, keys match case-insensitively, the last duplicate wins, and a
     * missing payload is {@code null}. The time keeps its offset and nanoseconds, and a missing time
     * is Go's zero time {@code 0001-01-01T00:00:00Z}.
     *
     * @param raw the stored event bytes
     * @param decodePayload the payload decoder
     * @param <P> the payload type
     * @return the event
     * @throws IllegalArgumentException for a non-object or an invalid time
     * @throws Exception when the payload decoder fails
     */
    public static <P> TriggerEvent<P> decodeTriggerEvent(final byte[] raw, final PayloadDecoder<P> decodePayload)
            throws Exception {
        String eventId = "";
        // Go decodes a missing time as its zero time.
        String occurredAt = Rfc3339NanoTimestamp.ZERO.toWire();
        byte[] payload = "null".getBytes(StandardCharsets.US_ASCII);
        try {
            if (GoRawJson.kind(raw) != GoRawJson.Kind.OBJECT) {
                throw new GoRawJson.InvalidRawJsonException();
            }
            for (Map.Entry<String, byte[]> member : GoRawJson.objectMembers(raw)) {
                if (GoRawJson.isGoFieldKey(member.getKey(), "eventId")) {
                    eventId = GoRawJson.decodeStringField(member.getValue(), eventId);
                } else if (GoRawJson.isGoFieldKey(member.getKey(), "occurredAt")) {
                    occurredAt = GoRawJson.decodeStringField(member.getValue(), occurredAt);
                } else if (GoRawJson.isGoFieldKey(member.getKey(), "payload")) {
                    payload = member.getValue();
                }
            }
        } catch (RuntimeException error) {
            throw new IllegalArgumentException("pending Trigger event is not a JSON object", error);
        }
        final OffsetDateTime time;
        try {
            time = Rfc3339NanoTimestamp.fromWire(occurredAt).toOffsetDateTime();
        } catch (RuntimeException error) {
            throw new IllegalArgumentException("pending Trigger event time is invalid", error);
        }
        final PayloadDecoder<P> decoder = decodePayload;
        return new TriggerEvent<P>(eventId, time, decoder.decode(GoRawJson.goText(payload), payload.clone()));
    }

    /**
     * Decodes one Trigger binding's configuration from a project configuration.
     *
     * <p>The binding is the {@code triggerBindings} entry whose four identity fields equal the key.
     *
     * @param configuration the project configuration
     * @param key the binding identity
     * @param valueType the configuration class; it should reject unknown fields
     * @param <C> the configuration type
     * @return the decoded configuration
     * @throws ObjectNotFoundException when the project has no such binding
     * @throws IllegalArgumentException when the configuration is not an object or cannot be decoded
     */
    public static <C> C decodeTriggerConfiguration(
            final Configuration configuration, final TriggerInboxKey key, final Class<C> valueType)
            throws ObjectNotFoundException {
        for (ObjectNode entry : configuration.getTriggerBindings()) {
            if (key.getConnectorId().equals(entry.path("connectorId").asText(null))
                    && key.getConnectionName().equals(entry.path("connectionName").asText(null))
                    && key.getTriggerName().equals(entry.path("triggerName").asText(null))
                    && key.getBindingName().equals(entry.path("bindingName").asText(null))) {
                final JsonNode value = entry.get("configuration");
                if (value == null || !value.isObject()) {
                    throw new IllegalArgumentException("trigger binding configuration must be an object");
                }
                try {
                    return PAYLOAD_MAPPER.treeToValue(value, valueType);
                } catch (IOException error) {
                    throw new IllegalArgumentException("trigger binding configuration is invalid", error);
                }
            }
        }
        throw new ObjectNotFoundException("trigger binding configuration not found");
    }
}
