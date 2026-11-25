// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.superdurable.dexconnectors.sdk.providerhttp.IpAddressLiterals;
import io.superdurable.dexconnectors.sdk.providerhttp.SplitUrl;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.regex.Pattern;

/** Go-compatible text rules used by manifest refresh declarations. */
final class GoText {
    private static final Pattern JWT_SEGMENT = Pattern.compile("[A-Za-z0-9_\\-\r\n]*=*");
    private static final int MAXIMUM_JWT_BYTES = 16 << 10;
    private static final ObjectMapper MAPPER = new ObjectMapper();

    private GoText() {
    }

    /** Trims the Unicode White_Space points Go {@code strings.TrimSpace} removes. */
    static String trimSpace(final String value) {
        int start = 0;
        int end = value.length();
        while (start < end && isGoSpace(value.charAt(start))) {
            start++;
        }
        while (end > start && isGoSpace(value.charAt(end - 1))) {
            end--;
        }
        return value.substring(start, end);
    }

    /** Matches Go 1.24 net/mail addr-spec cases whose parsed address equals its input. */
    static boolean isGoMailAddress(final String value) {
        final int at = value.indexOf('@');
        if (at < 0 || value.indexOf('@', at + 1) >= 0) {
            return false;
        }
        final String local = value.substring(0, at);
        final String domain = value.substring(at + 1);
        if (!isAtom(local)) {
            return false;
        }
        if (domain.startsWith("[") && domain.endsWith("]") && domain.length() >= 2) {
            return domain.indexOf('%') < 0 && IpAddressLiterals.isLiteral(domain.substring(1, domain.length() - 1));
        }
        return isAtom(domain);
    }

    /** Reads a JWT {@code exp} claim solely to schedule refresh. */
    static Long readJwtExpirySeconds(final String token) {
        if (token == null || token.isEmpty() || token.getBytes(StandardCharsets.UTF_8).length > MAXIMUM_JWT_BYTES) {
            return null;
        }
        final String[] segments = token.split("\\.", -1);
        if (segments.length != 3 || !JWT_SEGMENT.matcher(segments[1]).matches()) {
            return null;
        }
        String unpadded = segments[1];
        while (unpadded.endsWith("=")) {
            unpadded = unpadded.substring(0, unpadded.length() - 1);
        }
        unpadded = unpadded.replace("\r", "").replace("\n", "");
        if (unpadded.length() % 4 == 1) {
            return null;
        }
        try {
            final byte[] payload = Base64.getUrlDecoder().decode(unpadded);
            if (!hasBoundedJsonDepth(payload)) {
                return null;
            }
            final JsonNode claims = MAPPER.readTree(payload);
            if (claims == null || !claims.isObject()) {
                return null;
            }
            final JsonNode exp = claims.get("exp");
            if (exp == null || !exp.isNumber()) {
                return null;
            }
            final double seconds = exp.doubleValue();
            if (seconds <= 0 || seconds > 0x7FFFFFFFL * 4) {
                return null;
            }
            return exp.isIntegralNumber() ? exp.longValue() : (long) seconds;
        } catch (java.io.IOException | IllegalArgumentException error) {
            return null;
        }
    }

    /** Validates a mapped provider API origin; errors never contain the value. */
    static String validateResponseUrl(final String value, final JsonNode rule) {
        if (!value.startsWith("https://") && !value.startsWith("http://")) {
            throw new IllegalArgumentException("response URL is invalid");
        }
        for (int index = 0; index < value.length(); index++) {
            final char character = value.charAt(index);
            if (character <= 32 || character > 126 || character == '?' || character == '#') {
                throw new IllegalArgumentException("response URL is invalid");
            }
        }
        final SplitUrl parsed = SplitUrl.split(value);
        final String host = parsed.getHostname();
        final Integer port;
        try {
            port = parsed.getPort();
        } catch (IllegalArgumentException error) {
            throw new IllegalArgumentException("response URL is invalid");
        }
        final boolean isLoopback = IpAddressLiterals.isLiteral(host)
                ? IpAddressLiterals.isLoopbackLiteral(host) : "localhost".equals(host);
        final boolean allowsLoopback = rule.path("allowLoopback").asBoolean(false);
        if (host.isEmpty() || parsed.hasUserInfo()
                || !(parsed.getPath().isEmpty() || "/".equals(parsed.getPath()))
                || !parsed.getQuery().isEmpty() || !parsed.getFragment().isEmpty()
                || (!"https".equals(parsed.getScheme())
                && !(allowsLoopback && isLoopback && "http".equals(parsed.getScheme())))
                || (rule.path("forbidPort").asBoolean(false) && port != null)) {
            throw new IllegalArgumentException("response URL is invalid");
        }
        final String suffix = rule.path("allowedHostSuffix").asText("");
        boolean isAllowedHost = false;
        for (JsonNode allowed : rule.path("allowedHosts")) {
            if (allowed.isTextual() && allowed.textValue().equals(parsed.getNetloc())) {
                isAllowedHost = true;
            }
        }
        if (!((rule.path("allowAnyHTTPS").asBoolean(false) && "https".equals(parsed.getScheme()))
                || (allowsLoopback && isLoopback)
                || isAllowedHost
                || (!suffix.isEmpty() && (parsed.getNetloc().equals(suffix)
                || parsed.getNetloc().endsWith("." + suffix))))) {
            throw new IllegalArgumentException("response URL is invalid");
        }
        String trimmed = value;
        while (trimmed.endsWith("/")) {
            trimmed = trimmed.substring(0, trimmed.length() - 1);
        }
        return trimmed;
    }

    static boolean isValidResponseUrl(final String value, final JsonNode rule) {
        try {
            validateResponseUrl(value, rule);
            return true;
        } catch (IllegalArgumentException error) {
            return false;
        }
    }

    private static boolean hasBoundedJsonDepth(final byte[] payload) {
        if (payload.length > MAXIMUM_JWT_BYTES) {
            return false;
        }
        int depth = 0;
        boolean isInString = false;
        boolean isEscaped = false;
        for (byte value : payload) {
            if (isInString) {
                if (isEscaped) {
                    isEscaped = false;
                } else if (value == 0x5C) {
                    isEscaped = true;
                } else if (value == 0x22) {
                    isInString = false;
                }
            } else if (value == 0x22) {
                isInString = true;
            } else if (value == 0x7B || value == 0x5B) {
                depth++;
                if (depth > 128) {
                    return false;
                }
            } else if (value == 0x7D || value == 0x5D) {
                depth--;
                if (depth < 0) {
                    return false;
                }
            }
        }
        return depth == 0 && !isInString;
    }

    private static boolean isAtom(final String text) {
        if (text.isEmpty() || text.charAt(0) == '.' || text.charAt(text.length() - 1) == '.' || text.contains("..")) {
            return false;
        }
        for (int index = 0; index < text.length(); index++) {
            final char character = text.charAt(index);
            if (Character.isSurrogate(character)) {
                if (Character.isHighSurrogate(character) && index + 1 < text.length()
                        && Character.isLowSurrogate(text.charAt(index + 1))) {
                    index++;
                    continue;
                }
                return false;
            }
            if (character >= 0x80) {
                continue;
            }
            if (character < 0x21 || character > 0x7E || "()<>[]:;@\\,\"".indexOf(character) >= 0) {
                return false;
            }
        }
        return true;
    }

    private static boolean isGoSpace(final char character) {
        return character == 0x20 || (character >= 0x09 && character <= 0x0D)
                || character == 0x85 || character == 0xA0 || character == 0x1680
                || (character >= 0x2000 && character <= 0x200A)
                || character == 0x2028 || character == 0x2029 || character == 0x202F
                || character == 0x205F || character == 0x3000;
    }
}
