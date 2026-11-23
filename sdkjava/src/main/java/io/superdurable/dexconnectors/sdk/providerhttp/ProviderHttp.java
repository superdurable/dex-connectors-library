// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import com.fasterxml.jackson.core.JsonFactory;
import com.fasterxml.jackson.core.JsonParser;
import com.fasterxml.jackson.core.JsonToken;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.time.LocalDateTime;
import java.time.ZoneOffset;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.TreeMap;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Bounded, secret-safe helpers for provider HTTP requests and responses.
 */
public final class ProviderHttp {
    /** Largest provider error body read for classification, in bytes. */
    public static final int MAX_ERROR_BODY_BYTES = 64 << 10;
    /** Largest honored {@code Retry-After} delay. */
    public static final Duration MAX_RETRY_AFTER_DELAY = Duration.ofHours(1);

    private static final Pattern ERROR_TOKEN = Pattern.compile("^[A-Za-z0-9_.:-]{1,64}$");
    private static final Pattern HTTP_DATE = Pattern.compile(
            "^(?:[A-Za-z]{3},\\s*)?(\\d{1,2})\\s+([A-Za-z]{3})\\s+(\\d{2,4})\\s+(\\d{1,2}):(\\d{2})(?::(\\d{2}))?"
                    + "(?:\\s+(GMT|UTC|UT|Z|[+-]\\d{4}))?$");
    private static final List<String> MONTHS =
            Arrays.asList("jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec");
    private static final JsonFactory JSON_FACTORY = new JsonFactory();
    private static final Object JSON_NULL = new Object();

    private ProviderHttp() {
    }

    /**
     * Encodes a string multimap like Go {@code net/url.Values.Encode}.
     *
     * <p>Keys sort by UTF-8 bytes, repeated values keep their order, and empty value lists are
     * omitted.
     *
     * @param values the query values by key
     * @return the encoded query without {@code ?}
     */
    public static String encodeQueryValues(final Map<String, List<String>> values) {
        final TreeMap<byte[], String> sortedKeys = new TreeMap<>(ProviderHttp::compareUnsignedBytes);
        for (String key : values.keySet()) {
            sortedKeys.put(key.getBytes(StandardCharsets.UTF_8), key);
        }
        final StringBuilder encoded = new StringBuilder();
        for (String key : sortedKeys.values()) {
            final List<String> entries = values.get(key);
            if (entries == null) {
                continue;
            }
            for (String value : entries) {
                if (value == null) {
                    throw new IllegalArgumentException("query values must be a sequence of strings");
                }
                if (encoded.length() > 0) {
                    encoded.append('&');
                }
                encoded.append(encodeFormComponent(key)).append('=').append(encodeFormComponent(value));
            }
        }
        return encoded.toString();
    }

    /**
     * Percent-encodes a form component like Python {@code quote_plus}.
     *
     * <p>ASCII letters, digits, and {@code _.-~} stay literal, a space becomes {@code +}, and every
     * other UTF-8 byte becomes uppercase {@code %XX}.
     *
     * @param value the component
     * @return the encoded component
     */
    public static String encodeFormComponent(final String value) {
        return percentEncode(value, "_.-~", true);
    }

    /**
     * Percent-encodes text like Python {@code urllib.parse.quote} with the given safe characters.
     *
     * @param value the text
     * @param safeCharacters ASCII characters kept literal in addition to letters, digits, and {@code _.-~}
     * @return the encoded text
     */
    public static String percentEncode(final String value, final String safeCharacters) {
        return percentEncode(value, "_.-~" + safeCharacters, false);
    }

    /**
     * Reads safe scalar tokens at RFC 6901 pointers from a bounded JSON error body.
     *
     * <p>Numbers keep their original text. A token must match {@code [A-Za-z0-9_.:-]{1,64}};
     * other values, invalid JSON, and missing pointers yield nothing.
     *
     * @param body the error body
     * @param pointers the JSON Pointers to read, in order
     * @return the matching tokens, in pointer order
     */
    public static List<String> readErrorTokens(final byte[] body, final List<String> pointers) {
        final Object document;
        try (JsonParser parser = JSON_FACTORY.createParser(body)) {
            final JsonToken first = parser.nextToken();
            if (first == null) {
                return Collections.emptyList();
            }
            document = readValue(parser, first);
            if (parser.nextToken() != null) {
                return Collections.emptyList();
            }
        } catch (IOException | RuntimeException error) {
            return Collections.emptyList();
        }
        final List<String> tokens = new ArrayList<>();
        for (String pointer : pointers) {
            if (!pointer.isEmpty() && !pointer.startsWith("/")) {
                continue;
            }
            Object current = document;
            if (!pointer.isEmpty()) {
                for (String escaped : pointer.substring(1).split("/", -1)) {
                    final String segment = escaped.replace("~1", "/").replace("~0", "~");
                    if (current instanceof Map) {
                        current = ((Map<?, ?>) current).get(segment);
                    } else if (current instanceof List && isCanonicalIndex(segment)
                            && Long.parseLong(segment) < ((List<?>) current).size()) {
                        current = ((List<?>) current).get(Integer.parseInt(segment));
                    } else {
                        current = null;
                        break;
                    }
                }
            }
            if (current instanceof String && ERROR_TOKEN.matcher((String) current).matches()) {
                tokens.add((String) current);
            }
        }
        return tokens;
    }

    /**
     * Parses a {@code Retry-After} value as delta-seconds or an HTTP date, capped at one hour.
     *
     * @param value the header value
     * @param now the current time
     * @return the nonnegative delay; an invalid value gives zero
     */
    public static Duration parseRetryAfter(final String value, final Instant now) {
        final String trimmed = value == null ? "" : value.trim();
        if (!trimmed.isEmpty() && isAsciiDigits(trimmed)) {
            // Compare by value, so leading zeros and overflow both reach the cap only when the value does.
            final java.math.BigInteger seconds = new java.math.BigInteger(trimmed);
            if (seconds.compareTo(java.math.BigInteger.valueOf(MAX_RETRY_AFTER_DELAY.getSeconds())) > 0) {
                return MAX_RETRY_AFTER_DELAY;
            }
            return Duration.ofSeconds(seconds.longValue());
        }
        final Instant retryAt = parseHttpDate(trimmed);
        if (retryAt == null) {
            return Duration.ZERO;
        }
        final Duration delay = Duration.between(now, retryAt);
        if (delay.isNegative()) {
            return Duration.ZERO;
        }
        return delay.compareTo(MAX_RETRY_AFTER_DELAY) > 0 ? MAX_RETRY_AFTER_DELAY : delay;
    }

    /**
     * Requires a printable HTTPS base URL, allowing HTTP only for localhost or a loopback IP.
     *
     * @param value the base URL
     * @return the URL without query, fragment, or trailing path slashes
     * @throws IllegalArgumentException if the URL is not acceptable
     */
    public static String validateBaseUrl(final String value) {
        if (value == null || value.isEmpty()) {
            throw new IllegalArgumentException("base URL must be printable ASCII without spaces");
        }
        for (int index = 0; index < value.length(); index++) {
            if (value.charAt(index) < 33 || value.charAt(index) > 126) {
                throw new IllegalArgumentException("base URL must be printable ASCII without spaces");
            }
        }
        final SplitUrl parsed = SplitUrl.split(value);
        if (parsed.getHostname().isEmpty() || parsed.hasUserInfo() || value.indexOf('?') >= 0
                || value.indexOf('#') >= 0) {
            throw new IllegalArgumentException(
                    "base URL must be absolute without user information, query, or fragment");
        }
        final Integer port;
        try {
            port = parsed.getPort();
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("base URL port is invalid");
        }
        if (port != null && port == 0) {
            throw new IllegalArgumentException("base URL port is invalid");
        }
        final boolean isLoopback = "localhost".equals(parsed.getHostname())
                || IpAddressLiterals.isLoopbackLiteral(parsed.getHostname());
        if (!"https".equals(parsed.getScheme()) && !("http".equals(parsed.getScheme()) && isLoopback)) {
            throw new IllegalArgumentException("base URL must use HTTPS unless its host is loopback");
        }
        String path = parsed.getPath();
        while (path.endsWith("/")) {
            path = path.substring(0, path.length() - 1);
        }
        return parsed.getScheme() + "://" + parsed.getNetloc() + path;
    }

    /**
     * Reports whether a credential is nonempty printable ASCII without spaces or controls.
     *
     * @param value the credential
     * @return {@code true} when the value is safe in an HTTP header
     */
    public static boolean isHeaderSafeCredential(final String value) {
        if (value == null || value.isEmpty()) {
            return false;
        }
        for (int index = 0; index < value.length(); index++) {
            if (value.charAt(index) < 33 || value.charAt(index) > 126) {
                return false;
            }
        }
        return true;
    }

    /**
     * Returns the reason an address policy refuses a resolved address, or empty when allowed.
     *
     * @param address the IP literal
     * @param scheme {@code http} or {@code https}
     * @param policy the restricted policy
     * @return the secret-safe refusal reason, or empty
     */
    public static String addressRefusalReason(final String address, final String scheme, final AddressPolicy policy) {
        final byte[] parsed = IpAddressLiterals.parse(address);
        if (parsed == null) {
            return "resolved address is invalid";
        }
        return AddressPolicyRules.refusalReason(parsed, scheme, policy);
    }

    private static Object readValue(final JsonParser parser, final JsonToken token) throws IOException {
        switch (token) {
            case START_OBJECT: {
                final Map<String, Object> object = new LinkedHashMap<>();
                while (parser.nextToken() == JsonToken.FIELD_NAME) {
                    final String name = parser.currentName();
                    object.put(name, readValue(parser, parser.nextToken()));
                }
                return object;
            }
            case START_ARRAY: {
                final List<Object> array = new ArrayList<>();
                JsonToken next = parser.nextToken();
                while (next != JsonToken.END_ARRAY) {
                    array.add(readValue(parser, next));
                    next = parser.nextToken();
                }
                return array;
            }
            case VALUE_STRING:
            case VALUE_NUMBER_INT:
            case VALUE_NUMBER_FLOAT:
                return parser.getText();
            case VALUE_TRUE:
                return Boolean.TRUE;
            case VALUE_FALSE:
                return Boolean.FALSE;
            case VALUE_NULL:
                return JSON_NULL;
            default:
                throw new IOException("unexpected JSON token");
        }
    }

    private static Instant parseHttpDate(final String value) {
        final Matcher match = HTTP_DATE.matcher(value);
        if (!match.matches()) {
            return null;
        }
        final int month = MONTHS.indexOf(match.group(2).toLowerCase(Locale.ROOT)) + 1;
        if (month == 0) {
            return null;
        }
        int year = Integer.parseInt(match.group(3));
        if (match.group(3).length() == 2) {
            year += year < 50 ? 2000 : 1900;
        }
        final String zone = match.group(7);
        final ZoneOffset offset;
        if (zone == null || zone.length() <= 3) {
            offset = ZoneOffset.UTC;
        } else {
            final int sign = zone.charAt(0) == '-' ? -1 : 1;
            final int hours = Integer.parseInt(zone.substring(1, 3));
            final int minutes = Integer.parseInt(zone.substring(3, 5));
            if (hours > 18 || minutes > 59) {
                return null;
            }
            offset = ZoneOffset.ofTotalSeconds(sign * (hours * 3600 + minutes * 60));
        }
        try {
            return LocalDateTime.of(year, month, Integer.parseInt(match.group(1)), Integer.parseInt(match.group(4)),
                    Integer.parseInt(match.group(5)), match.group(6) == null ? 0 : Integer.parseInt(match.group(6)))
                    .toInstant(offset);
        } catch (java.time.DateTimeException error) {
            return null;
        }
    }

    private static boolean isCanonicalIndex(final String segment) {
        if (segment.isEmpty() || segment.length() > 9 || !isAsciiDigits(segment)) {
            return false;
        }
        return segment.equals(Long.toString(Long.parseLong(segment)));
    }

    private static boolean isAsciiDigits(final String value) {
        for (int index = 0; index < value.length(); index++) {
            if (value.charAt(index) < '0' || value.charAt(index) > '9') {
                return false;
            }
        }
        return !value.isEmpty();
    }

    private static String percentEncode(final String value, final String safeCharacters, final boolean isForm) {
        final StringBuilder encoded = new StringBuilder();
        for (byte raw : value.getBytes(StandardCharsets.UTF_8)) {
            final int unsigned = raw & 0xff;
            final char character = (char) unsigned;
            if ((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
                    || (character >= '0' && character <= '9')
                    || (unsigned < 128 && safeCharacters.indexOf(character) >= 0)) {
                encoded.append(character);
            } else if (isForm && character == ' ') {
                encoded.append('+');
            } else {
                encoded.append('%').append(Character.toUpperCase(Character.forDigit(unsigned >> 4, 16)))
                        .append(Character.toUpperCase(Character.forDigit(unsigned & 0xf, 16)));
            }
        }
        return encoded.toString();
    }

    private static int compareUnsignedBytes(final byte[] left, final byte[] right) {
        final int length = Math.min(left.length, right.length);
        for (int index = 0; index < length; index++) {
            final int difference = (left[index] & 0xff) - (right[index] & 0xff);
            if (difference != 0) {
                return difference;
            }
        }
        return left.length - right.length;
    }
}
