// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.Rfc3339NanoTimestamp;
import java.io.IOException;
import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Encodes project documents as the Go Connector SDK's {@code encoding/json} writes them.
 *
 * <p>Struct-like objects keep their member order, while the free-form {@code configuration},
 * {@code credentials}, and {@code event} maps and the {@code environment} map sort their keys by
 * UTF-8 bytes, as Go sorts map keys. A {@code configuration}, {@code credentials}, or {@code event}
 * object read by {@link #parse} is a Go {@code json.RawMessage}: while unchanged it is written as its
 * compacted stored bytes, keeping member order and number spelling. Output is compact, writes {@code <}, {@code >}, {@code &},
 * U+2028, and U+2029 as lowercase Unicode escapes such as {@code \\u003c}, canonicalizes RFC 3339 timestamp fields,
 * and omits the empty optional fields Go omits. These bytes feed object digests and CAS ETags, so
 * they must match Go, Python, and TypeScript exactly.
 */
public final class GoCompatibleJson {
    private static final ObjectMapper MAPPER = new ObjectMapper()
            .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS);
    private static final Set<String> MAP_FIELDS = new HashSet<>(Arrays.asList("configuration", "credentials", "event"));
    private static final Set<String> TIME_FIELDS = new HashSet<>(
            Arrays.asList("expiresAt", "deadline", "issuedAt", "credentialExpiresAt", "observedAt"));
    private static final Set<String> NULL_OMITTED_FIELDS = new HashSet<>(
            Arrays.asList("expiresAt", "credential", "mutation", "credentialBroker", "secretRef"));
    private static final Set<String> EMPTY_OMITTED_FIELDS = new HashSet<>(
            Arrays.asList("authMethodId", "sharedConnectionId", "attemptId"));
    private static final char[] HEX = "0123456789abcdef".toCharArray();

    private enum Context {
        STRUCTURED,
        MAP,
        ENVIRONMENT
    }

    private GoCompatibleJson() {
    }

    /**
     * Encodes one project document object as Go-compatible canonical bytes.
     *
     * @param document the JSON object
     * @return the compact UTF-8 bytes
     * @throws IllegalArgumentException if the value is not an object or holds a non-finite number
     *     or an invalid timestamp
     */
    public static byte[] encode(final JsonNode document) {
        if (document == null || !document.isObject()) {
            throw new IllegalArgumentException("project document must be a JSON object");
        }
        final StringBuilder output = new StringBuilder();
        writeValue(output, document, Context.STRUCTURED);
        return output.toString().getBytes(StandardCharsets.UTF_8);
    }

    static byte[] encodeSortedMap(final JsonNode value) {
        final StringBuilder output = new StringBuilder();
        writeValue(output, value, Context.MAP);
        return output.toString().getBytes(StandardCharsets.UTF_8);
    }

    /**
     * Returns the shared {@code sha256:}-prefixed lowercase hexadecimal object digest.
     *
     * @param contents the bytes
     * @return the digest
     */
    public static String digestBytes(final byte[] contents) {
        return "sha256:" + sha256Hex(contents);
    }

    /**
     * Returns the lowercase hexadecimal SHA-256 of the bytes.
     *
     * @param contents the bytes
     * @return the hexadecimal digest
     */
    public static String sha256Hex(final byte[] contents) {
        final MessageDigest digest;
        try {
            digest = MessageDigest.getInstance("SHA-256");
        } catch (NoSuchAlgorithmException error) {
            throw new IllegalStateException("SHA-256 is required by every Java platform", error);
        }
        final byte[] hash = digest.digest(contents);
        final char[] text = new char[hash.length * 2];
        for (int index = 0; index < hash.length; index++) {
            text[index * 2] = HEX[(hash[index] >> 4) & 0xf];
            text[index * 2 + 1] = HEX[hash[index] & 0xf];
        }
        return new String(text);
    }

    /**
     * Parses one complete JSON document.
     *
     * @param contents the UTF-8 bytes
     * @return the parsed value
     * @throws IllegalArgumentException if the bytes are not one JSON value
     */
    public static JsonNode parse(final byte[] contents) {
        JsonNode value;
        try {
            value = MAPPER.readTree(contents);
        } catch (IOException | RuntimeException error) {
            // Parser messages and causes can quote private file contents, such as a token.
            value = null;
        }
        if (value == null || value.isMissingNode()) {
            throw new IllegalArgumentException("project document is invalid JSON");
        }
        return preserveRawMessages(contents, value);
    }

    static JsonNode parseWithoutRawMessages(final String text) {
        try {
            final JsonNode value = MAPPER.readTree(text);
            if (value == null || value.isMissingNode()) {
                throw new IllegalArgumentException("project document is invalid JSON");
            }
            return value;
        } catch (IOException error) {
            throw new IllegalArgumentException("project document is invalid JSON");
        }
    }

    // Go declares configuration, credentials, and event as json.RawMessage, so it re-encodes their bytes.
    private static JsonNode preserveRawMessages(final byte[] raw, final JsonNode value) {
        try {
            attachRawMessages(raw, value);
        } catch (RuntimeException error) {
            // Go's scanner refuses what Jackson accepted; keep the tree, which re-encodes as a map.
        }
        return value;
    }

    private static void attachRawMessages(final byte[] raw, final JsonNode value) {
        if (value.isObject()) {
            final ObjectNode object = (ObjectNode) value;
            for (Map.Entry<String, byte[]> member : GoRawJson.objectMembers(raw)) {
                final JsonNode child = object.get(member.getKey());
                if (child == null) {
                    continue;
                }
                if (MAP_FIELDS.contains(member.getKey()) && child.isObject()
                        && GoRawJson.kind(member.getValue()) == GoRawJson.Kind.OBJECT) {
                    object.set(member.getKey(), new RawMessageObjectNode(
                            new String(GoRawJson.compact(member.getValue()), StandardCharsets.UTF_8), (ObjectNode) child));
                } else if (child.isContainerNode()) {
                    attachRawMessages(member.getValue(), child);
                }
            }
        } else if (value.isArray()) {
            final List<byte[]> elements = GoRawJson.arrayElements(raw);
            for (int index = 0; index < elements.size() && index < value.size(); index++) {
                if (value.get(index).isContainerNode()) {
                    attachRawMessages(elements.get(index), value.get(index));
                }
            }
        }
    }

    /**
     * Parses one JSON object.
     *
     * @param contents the UTF-8 bytes
     * @param label the document name used in errors
     * @return the parsed object
     * @throws IllegalArgumentException if the bytes are not one JSON object
     */
    public static ObjectNode parseObject(final byte[] contents, final String label) {
        return requireObject(parse(contents), label);
    }

    /**
     * Requires a JSON object.
     *
     * @param value the value
     * @param label the document name used in errors
     * @return the object
     * @throws IllegalArgumentException if the value is not an object
     */
    public static ObjectNode requireObject(final JsonNode value, final String label) {
        if (value == null || !value.isObject()) {
            throw new IllegalArgumentException(label + " must be a JSON object");
        }
        return (ObjectNode) value;
    }

    /**
     * Returns a new empty object node.
     *
     * @return the object
     */
    public static ObjectNode newObject() {
        return JsonNodeFactory.instance.objectNode();
    }

    /**
     * Converts a value to a JSON tree through the default Jackson mapper.
     *
     * @param value the value
     * @return the tree
     */
    public static JsonNode toTree(final Object value) {
        return MAPPER.valueToTree(value);
    }

    /**
     * Writes a value as ordinary compact JSON text.
     *
     * @param value the value
     * @return the JSON text
     */
    public static String toJsonText(final Object value) {
        try {
            return MAPPER.writeValueAsString(value);
        } catch (JsonProcessingException error) {
            throw new IllegalArgumentException("value cannot be encoded as JSON", error);
        }
    }

    private static void writeValue(final StringBuilder output, final JsonNode value, final Context context) {
        if (value == null || value.isNull() || value.isMissingNode()) {
            output.append("null");
        } else if (value.isBoolean()) {
            output.append(value.booleanValue() ? "true" : "false");
        } else if (value.isIntegralNumber()) {
            output.append(value.bigIntegerValue().toString());
        } else if (value.isNumber()) {
            output.append(formatGoFloat(value.doubleValue()));
        } else if (value.isTextual()) {
            writeString(output, value.textValue());
        } else if (value.isArray()) {
            output.append('[');
            for (int index = 0; index < value.size(); index++) {
                if (index > 0) {
                    output.append(',');
                }
                writeValue(output, value.get(index), context);
            }
            output.append(']');
        } else if (value instanceof RawMessageObjectNode
                && ((RawMessageObjectNode) value).unchangedCompactedText() != null) {
            output.append(((RawMessageObjectNode) value).unchangedCompactedText());
        } else if (value.isObject()) {
            writeObject(output, value, context);
        } else {
            throw new IllegalArgumentException("project document contains a non-JSON value");
        }
    }

    private static void writeObject(final StringBuilder output, final JsonNode object, final Context context) {
        final boolean isStructured = context == Context.STRUCTURED;
        final List<String> keys = new ArrayList<>();
        final Iterator<String> names = object.fieldNames();
        while (names.hasNext()) {
            keys.add(names.next());
        }
        if (!isStructured) {
            keys.sort(GoCompatibleJson::compareUtf8);
        }
        output.append('{');
        boolean isFirst = true;
        for (String key : keys) {
            final JsonNode fieldValue = object.get(key);
            if (isStructured && shouldOmitGoField(key, fieldValue)) {
                continue;
            }
            if (!isFirst) {
                output.append(',');
            }
            isFirst = false;
            final Context childContext;
            if (context == Context.ENVIRONMENT) {
                childContext = Context.STRUCTURED;
            } else if (context == Context.MAP) {
                childContext = Context.MAP;
            } else if ("environment".equals(key)) {
                childContext = Context.ENVIRONMENT;
            } else if (MAP_FIELDS.contains(key)) {
                childContext = Context.MAP;
            } else {
                childContext = Context.STRUCTURED;
            }
            writeString(output, key);
            output.append(':');
            if (isStructured && TIME_FIELDS.contains(key) && fieldValue.isTextual()) {
                writeString(output, Rfc3339NanoTimestamp.fromWire(fieldValue.textValue()).toWire());
            } else {
                writeValue(output, fieldValue, childContext);
            }
        }
        output.append('}');
    }

    private static boolean shouldOmitGoField(final String key, final JsonNode value) {
        if (value == null || value.isMissingNode()) {
            return true;
        }
        if (NULL_OMITTED_FIELDS.contains(key)) {
            return value.isNull();
        }
        if (EMPTY_OMITTED_FIELDS.contains(key)) {
            return value.isTextual() && value.textValue().isEmpty();
        }
        if ("environment".equals(key)) {
            return value.isNull() || (value.isObject() && value.size() == 0);
        }
        return false;
    }

    static int compareUtf8(final String left, final String right) {
        final byte[] leftBytes = wellFormed(left).getBytes(StandardCharsets.UTF_8);
        final byte[] rightBytes = wellFormed(right).getBytes(StandardCharsets.UTF_8);
        final int length = Math.min(leftBytes.length, rightBytes.length);
        for (int index = 0; index < length; index++) {
            final int difference = (leftBytes[index] & 0xff) - (rightBytes[index] & 0xff);
            if (difference != 0) {
                return difference;
            }
        }
        return leftBytes.length - rightBytes.length;
    }

    static String wellFormed(final String text) {
        StringBuilder replaced = null;
        for (int index = 0; index < text.length(); index++) {
            final char character = text.charAt(index);
            final boolean isPaired = Character.isHighSurrogate(character) && index + 1 < text.length()
                    && Character.isLowSurrogate(text.charAt(index + 1));
            if (isPaired) {
                if (replaced != null) {
                    replaced.append(character).append(text.charAt(index + 1));
                }
                index++;
                continue;
            }
            if (Character.isSurrogate(character)) {
                if (replaced == null) {
                    replaced = new StringBuilder(text.substring(0, index));
                }
                replaced.append('�');
            } else if (replaced != null) {
                replaced.append(character);
            }
        }
        return replaced == null ? text : replaced.toString();
    }

    static void writeString(final StringBuilder output, final String rawText) {
        final String text = wellFormed(rawText);
        output.append('"');
        for (int index = 0; index < text.length(); index++) {
            final char character = text.charAt(index);
            switch (character) {
                case '"':
                    output.append("\\\"");
                    break;
                case '\\':
                    output.append("\\\\");
                    break;
                case '\n':
                    output.append("\\n");
                    break;
                case '\r':
                    output.append("\\r");
                    break;
                case '\t':
                    output.append("\\t");
                    break;
                case '\b':
                    output.append("\\b");
                    break;
                case '\f':
                    output.append("\\f");
                    break;
                case '<':
                case '>':
                case '&':
                case ' ':
                case ' ':
                    appendUnicodeEscape(output, character);
                    break;
                default:
                    if (character < 0x20) {
                        appendUnicodeEscape(output, character);
                    } else {
                        output.append(character);
                    }
            }
        }
        output.append('"');
    }

    private static void appendUnicodeEscape(final StringBuilder output, final char character) {
        output.append("\\u")
                .append(HEX[(character >> 12) & 0xf])
                .append(HEX[(character >> 8) & 0xf])
                .append(HEX[(character >> 4) & 0xf])
                .append(HEX[character & 0xf]);
    }

    static String formatGoFloat(final double value) {
        if (Double.isNaN(value) || Double.isInfinite(value)) {
            throw new IllegalArgumentException("project document contains a non-finite number");
        }
        if (value == 0) {
            return "0";
        }
        final BigDecimal decimal = new BigDecimal(Double.toString(Math.abs(value))).stripTrailingZeros();
        final String digits = decimal.unscaledValue().toString();
        final int count = digits.length();
        final int point = count - decimal.scale();
        final String prefix = value < 0 ? "-" : "";
        if (count <= point && point <= 21) {
            final StringBuilder zeros = new StringBuilder();
            for (int index = count; index < point; index++) {
                zeros.append('0');
            }
            return prefix + digits + zeros;
        }
        if (0 < point && point <= 21) {
            return prefix + digits.substring(0, point) + "." + digits.substring(point);
        }
        if (-6 < point && point <= 0) {
            final StringBuilder zeros = new StringBuilder();
            for (int index = 0; index < -point; index++) {
                zeros.append('0');
            }
            return prefix + "0." + zeros + digits;
        }
        final int scientific = point - 1;
        final String mantissa = count == 1 ? digits : digits.charAt(0) + "." + digits.substring(1);
        return prefix + mantissa + "e" + (scientific >= 0 ? "+" : "-") + Math.abs(scientific);
    }
}
