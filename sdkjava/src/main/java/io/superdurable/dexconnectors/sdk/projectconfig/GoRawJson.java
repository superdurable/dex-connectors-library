// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.AbstractMap;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;

/**
 * Go {@code encoding/json} emulation over raw bytes for documents shared with Go replicas.
 *
 * <p>Every scanner is iterative, so Go's 10000-level nesting limit, not the Java stack, bounds it.
 */
final class GoRawJson {
    private static final int MAXIMUM_NESTING_DEPTH = 10_000;
    private static final int QUOTE = 0x22;
    private static final int BACKSLASH = 0x5C;
    private static final int COMMA = 0x2C;
    private static final int COLON = 0x3A;
    private static final int OPEN_BRACE = 0x7B;
    private static final int CLOSE_BRACE = 0x7D;
    private static final int OPEN_BRACKET = 0x5B;
    private static final int CLOSE_BRACKET = 0x5D;

    enum Kind {
        OBJECT,
        ARRAY,
        STRING,
        NUMBER,
        TRUE,
        FALSE,
        NULL
    }

    /** One complete value is not valid JSON, or Go cannot encode it. */
    static final class InvalidRawJsonException extends IllegalArgumentException {
        private static final long serialVersionUID = 1L;

        InvalidRawJsonException() {
            super("raw JSON is invalid");
        }
    }

    private GoRawJson() {
    }

    static Kind kind(final byte[] raw) {
        final int start = wholeValueStart(raw);
        switch (raw[start]) {
            case OPEN_BRACE:
                return Kind.OBJECT;
            case OPEN_BRACKET:
                return Kind.ARRAY;
            case QUOTE:
                return Kind.STRING;
            case 't':
                return Kind.TRUE;
            case 'f':
                return Kind.FALSE;
            case 'n':
                return Kind.NULL;
            default:
                return Kind.NUMBER;
        }
    }

    static byte[] trimLeadingWhitespace(final byte[] raw) {
        final int start = skipWhitespace(raw, 0);
        return Arrays.copyOfRange(raw, start, raw.length);
    }

    /** Validates one value and compacts it as Go writes a {@code json.RawMessage}. */
    static byte[] compact(final byte[] raw) {
        final int start = wholeValueStart(raw);
        final int end = scanValue(raw, start);
        final ByteArrayOutputStream output = new ByteArrayOutputStream(end - start);
        boolean isInString = false;
        int index = start;
        while (index < end) {
            final int value = raw[index] & 0xff;
            if (!isInString) {
                if (!isWhitespace(value)) {
                    output.write(value);
                    isInString = value == QUOTE;
                }
                index++;
                continue;
            }
            if (value == BACKSLASH) {
                output.write(raw, index, 2);
                index += 2;
                continue;
            }
            if (value == QUOTE) {
                isInString = false;
                output.write(value);
                index++;
                continue;
            }
            if (value == '<' || value == '>' || value == '&') {
                final byte[] escape = String.format(java.util.Locale.ROOT, "\\u00%x", value).getBytes(StandardCharsets.US_ASCII);
                output.write(escape, 0, escape.length);
                index++;
                continue;
            }
            if (value == 0xE2 && index + 2 < end && (raw[index + 1] & 0xff) == 0x80
                    && ((raw[index + 2] & 0xff) == 0xA8 || (raw[index + 2] & 0xff) == 0xA9)) {
                final byte[] escape = ((raw[index + 2] & 0xff) == 0xA8 ? "\\u2028" : "\\u2029")
                        .getBytes(StandardCharsets.US_ASCII);
                output.write(escape, 0, escape.length);
                index += 3;
                continue;
            }
            output.write(value);
            index++;
        }
        return output.toByteArray();
    }

    /** Splits one object into decoded keys and raw values in document order, duplicates kept. */
    static List<Map.Entry<String, byte[]>> objectMembers(final byte[] raw) {
        int index = skipWhitespace(raw, 0);
        if (byteAt(raw, index) != OPEN_BRACE) {
            throw new InvalidRawJsonException();
        }
        final List<Map.Entry<String, byte[]>> members = new ArrayList<>();
        index = skipWhitespace(raw, index + 1);
        if (byteAt(raw, index) == CLOSE_BRACE) {
            finishContainer(raw, index);
            return members;
        }
        while (true) {
            final int keyEnd = scanString(raw, index);
            final String key = decodeString(Arrays.copyOfRange(raw, index, keyEnd));
            index = skipWhitespace(raw, keyEnd);
            if (byteAt(raw, index) != COLON) {
                throw new InvalidRawJsonException();
            }
            final int valueStart = skipWhitespace(raw, index + 1);
            final int valueEnd = scanValue(raw, valueStart);
            members.add(new AbstractMap.SimpleImmutableEntry<>(key, Arrays.copyOfRange(raw, valueStart, valueEnd)));
            index = skipWhitespace(raw, valueEnd);
            if (byteAt(raw, index) == CLOSE_BRACE) {
                finishContainer(raw, index);
                return members;
            }
            if (byteAt(raw, index) != COMMA) {
                throw new InvalidRawJsonException();
            }
            index = skipWhitespace(raw, index + 1);
        }
    }

    /** Splits one array into the raw bytes of its elements. */
    static List<byte[]> arrayElements(final byte[] raw) {
        int index = skipWhitespace(raw, 0);
        if (byteAt(raw, index) != OPEN_BRACKET) {
            throw new InvalidRawJsonException();
        }
        final List<byte[]> elements = new ArrayList<>();
        index = skipWhitespace(raw, index + 1);
        if (byteAt(raw, index) == CLOSE_BRACKET) {
            finishContainer(raw, index);
            return elements;
        }
        while (true) {
            final int end = scanValue(raw, index);
            elements.add(Arrays.copyOfRange(raw, index, end));
            index = skipWhitespace(raw, end);
            if (byteAt(raw, index) == CLOSE_BRACKET) {
                finishContainer(raw, index);
                return elements;
            }
            if (byteAt(raw, index) != COMMA) {
                throw new InvalidRawJsonException();
            }
            index = skipWhitespace(raw, index + 1);
        }
    }

    /** Decodes one JSON string, quotes included; invalid UTF-8 and lone surrogates become U+FFFD. */
    static String decodeString(final byte[] raw) {
        if (byteAt(raw, 0) != QUOTE || scanString(raw, 0) != raw.length) {
            throw new InvalidRawJsonException();
        }
        final StringBuilder characters = new StringBuilder();
        int index = 1;
        final int end = raw.length - 1;
        while (index < end) {
            final int value = raw[index] & 0xff;
            if (value == BACKSLASH) {
                final int escape = raw[index + 1] & 0xff;
                if (escape != 'u') {
                    characters.append((char) simpleEscape(escape));
                    index += 2;
                    continue;
                }
                int unit = hexUnit(raw, index + 2);
                index += 6;
                if (unit >= 0xD800 && unit <= 0xDFFF) {
                    final boolean isEscapeNext = byteAt(raw, index) == BACKSLASH && byteAt(raw, index + 1) == 'u';
                    final int low = isEscapeNext ? hexUnit(raw, index + 2) : -1;
                    if (unit <= 0xDBFF && low >= 0xDC00 && low <= 0xDFFF) {
                        unit = 0x10000 + ((unit - 0xD800) << 10) + (low - 0xDC00);
                        index += 6;
                    } else {
                        unit = 0xFFFD;
                    }
                }
                characters.appendCodePoint(unit);
                continue;
            }
            if (value < 0x80) {
                characters.append((char) value);
                index++;
                continue;
            }
            final int[] rune = decodeRune(raw, index);
            characters.appendCodePoint(rune[0]);
            index += rune[1];
        }
        return characters.toString();
    }

    /** Decodes bytes as UTF-8, replacing each invalid byte with U+FFFD as Go does. */
    static String goText(final byte[] raw) {
        final StringBuilder characters = new StringBuilder(raw.length);
        int index = 0;
        while (index < raw.length) {
            final int value = raw[index] & 0xff;
            if (value < 0x80) {
                characters.append((char) value);
                index++;
                continue;
            }
            final int[] rune = decodeRune(raw, index);
            characters.appendCodePoint(rune[0]);
            index += rune[1];
        }
        return characters.toString();
    }

    static String encodeString(final String text) {
        final StringBuilder output = new StringBuilder();
        GoCompatibleJson.writeString(output, text);
        return output.toString();
    }

    /** Matches a key to a Go struct field as encoding/json does, with simple case folding. */
    static boolean isGoFieldKey(final String key, final String fieldName) {
        return key.equals(fieldName) || key.toLowerCase(java.util.Locale.ROOT).replace('ſ', 's')
                .replace('K', 'k').equals(fieldName.toLowerCase(java.util.Locale.ROOT));
    }

    static String decodeStringField(final byte[] raw, final String current) {
        final Kind kind = kind(raw);
        if (kind == Kind.NULL) {
            return current;
        }
        if (kind != Kind.STRING) {
            throw new InvalidRawJsonException();
        }
        return decodeString(trimTrailing(trimLeadingWhitespace(raw)));
    }

    private static byte[] trimTrailing(final byte[] raw) {
        int end = raw.length;
        while (end > 0 && isWhitespace(raw[end - 1] & 0xff)) {
            end--;
        }
        return Arrays.copyOf(raw, end);
    }

    private static int wholeValueStart(final byte[] raw) {
        final int start = skipWhitespace(raw, 0);
        final int end = scanValue(raw, start);
        if (skipWhitespace(raw, end) != raw.length) {
            throw new InvalidRawJsonException();
        }
        return start;
    }

    private static void finishContainer(final byte[] raw, final int closeIndex) {
        if (skipWhitespace(raw, closeIndex + 1) != raw.length) {
            throw new InvalidRawJsonException();
        }
    }

    private static int scanValue(final byte[] raw, final int start) {
        final List<Integer> containers = new ArrayList<>();
        int index = start;
        while (true) {
            index = skipWhitespace(raw, index);
            final int value = byteAt(raw, index);
            if (value == OPEN_BRACE || value == OPEN_BRACKET) {
                if (containers.size() >= MAXIMUM_NESTING_DEPTH) {
                    throw new InvalidRawJsonException();
                }
                containers.add(value);
                index = skipWhitespace(raw, index + 1);
                final int close = value == OPEN_BRACE ? CLOSE_BRACE : CLOSE_BRACKET;
                if (byteAt(raw, index) == close) {
                    containers.remove(containers.size() - 1);
                    index++;
                } else if (value == OPEN_BRACE) {
                    index = expectKey(raw, index);
                    continue;
                } else {
                    continue;
                }
            } else if (value == QUOTE) {
                index = scanString(raw, index);
            } else if (value == 't') {
                index = scanLiteral(raw, index, "true");
            } else if (value == 'f') {
                index = scanLiteral(raw, index, "false");
            } else if (value == 'n') {
                index = scanLiteral(raw, index, "null");
            } else {
                index = scanNumber(raw, index);
            }
            // A complete value ends here; close finished containers, then continue with the next sibling.
            while (true) {
                if (containers.isEmpty()) {
                    return index;
                }
                index = skipWhitespace(raw, index);
                final int container = containers.get(containers.size() - 1);
                final int close = container == OPEN_BRACE ? CLOSE_BRACE : CLOSE_BRACKET;
                if (byteAt(raw, index) == close) {
                    containers.remove(containers.size() - 1);
                    index++;
                    continue;
                }
                if (byteAt(raw, index) != COMMA) {
                    throw new InvalidRawJsonException();
                }
                index = skipWhitespace(raw, index + 1);
                if (container == OPEN_BRACE) {
                    index = expectKey(raw, index);
                }
                break;
            }
        }
    }

    private static int expectKey(final byte[] raw, final int start) {
        final int index = skipWhitespace(raw, scanString(raw, start));
        if (byteAt(raw, index) != COLON) {
            throw new InvalidRawJsonException();
        }
        return index + 1;
    }

    private static int scanString(final byte[] raw, final int start) {
        if (byteAt(raw, start) != QUOTE) {
            throw new InvalidRawJsonException();
        }
        int index = start + 1;
        while (index < raw.length) {
            final int value = raw[index] & 0xff;
            if (value == QUOTE) {
                return index + 1;
            }
            if (value < 0x20) {
                throw new InvalidRawJsonException();
            }
            if (value == BACKSLASH) {
                index++;
                final int escape = byteAt(raw, index);
                if (escape == 'u') {
                    if (hexUnit(raw, index + 1) < 0) {
                        throw new InvalidRawJsonException();
                    }
                    index += 4;
                } else if (simpleEscape(escape) < 0) {
                    throw new InvalidRawJsonException();
                }
            }
            index++;
        }
        throw new InvalidRawJsonException();
    }

    private static int scanLiteral(final byte[] raw, final int start, final String literal) {
        final byte[] expected = literal.getBytes(StandardCharsets.US_ASCII);
        if (start + expected.length > raw.length
                || !Arrays.equals(Arrays.copyOfRange(raw, start, start + expected.length), expected)) {
            throw new InvalidRawJsonException();
        }
        return start + expected.length;
    }

    private static int scanNumber(final byte[] raw, final int start) {
        int index = start;
        if (byteAt(raw, index) == '-') {
            index++;
        }
        if (byteAt(raw, index) == '0') {
            index++;
        } else if (isDigit(raw, index)) {
            while (isDigit(raw, index)) {
                index++;
            }
        } else {
            throw new InvalidRawJsonException();
        }
        if (byteAt(raw, index) == '.') {
            index++;
            if (!isDigit(raw, index)) {
                throw new InvalidRawJsonException();
            }
            while (isDigit(raw, index)) {
                index++;
            }
        }
        if (byteAt(raw, index) == 'e' || byteAt(raw, index) == 'E') {
            index++;
            if (byteAt(raw, index) == '+' || byteAt(raw, index) == '-') {
                index++;
            }
            if (!isDigit(raw, index)) {
                throw new InvalidRawJsonException();
            }
            while (isDigit(raw, index)) {
                index++;
            }
        }
        return index;
    }

    private static int[] decodeRune(final byte[] raw, final int index) {
        final int first = raw[index] & 0xff;
        if (first >= 0xC2 && first <= 0xDF) {
            final int second = continuation(raw, index + 1, 0x80, 0xBF);
            return second < 0 ? new int[] {0xFFFD, 1} : new int[] {((first & 0x1F) << 6) | second, 2};
        }
        if (first >= 0xE0 && first <= 0xEF) {
            final int second = continuation(raw, index + 1, first == 0xE0 ? 0xA0 : 0x80, first == 0xED ? 0x9F : 0xBF);
            final int third = second < 0 ? -1 : continuation(raw, index + 2, 0x80, 0xBF);
            return third < 0 ? new int[] {0xFFFD, 1}
                    : new int[] {((first & 0x0F) << 12) | (second << 6) | third, 3};
        }
        if (first >= 0xF0 && first <= 0xF4) {
            final int second = continuation(raw, index + 1, first == 0xF0 ? 0x90 : 0x80, first == 0xF4 ? 0x8F : 0xBF);
            final int third = second < 0 ? -1 : continuation(raw, index + 2, 0x80, 0xBF);
            final int fourth = third < 0 ? -1 : continuation(raw, index + 3, 0x80, 0xBF);
            if (fourth < 0) {
                return new int[] {0xFFFD, 1};
            }
            return new int[] {((first & 0x07) << 18) | (second << 12) | (third << 6) | fourth, 4};
        }
        return new int[] {0xFFFD, 1};
    }

    private static int continuation(final byte[] raw, final int index, final int low, final int high) {
        final int value = byteAt(raw, index);
        return value >= low && value <= high ? value & 0x3F : -1;
    }

    private static int simpleEscape(final int escape) {
        switch (escape) {
            case '"':
                return '"';
            case '\\':
                return '\\';
            case '/':
                return '/';
            case 'b':
                return '\b';
            case 'f':
                return '\f';
            case 'n':
                return '\n';
            case 'r':
                return '\r';
            case 't':
                return '\t';
            default:
                return -1;
        }
    }

    private static int hexUnit(final byte[] raw, final int index) {
        if (index + 4 > raw.length) {
            return -1;
        }
        int unit = 0;
        for (int offset = 0; offset < 4; offset++) {
            final int digit = Character.digit(raw[index + offset] & 0xff, 16);
            if (digit < 0) {
                return -1;
            }
            unit = unit * 16 + digit;
        }
        return unit;
    }

    private static boolean isDigit(final byte[] raw, final int index) {
        final int value = byteAt(raw, index);
        return value >= '0' && value <= '9';
    }

    private static int skipWhitespace(final byte[] raw, final int start) {
        int index = start;
        while (index < raw.length && isWhitespace(raw[index] & 0xff)) {
            index++;
        }
        return index;
    }

    private static boolean isWhitespace(final int value) {
        return value == ' ' || value == '\t' || value == '\n' || value == '\r';
    }

    private static int byteAt(final byte[] raw, final int index) {
        return index >= 0 && index < raw.length ? raw[index] & 0xff : -1;
    }
}
