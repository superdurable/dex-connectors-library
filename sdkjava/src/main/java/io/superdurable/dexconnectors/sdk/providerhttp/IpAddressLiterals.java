// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.net.InetAddress;
import java.net.UnknownHostException;

/**
 * Parses and classifies IP address literals without DNS lookups.
 *
 * <p>Parsing follows Python's {@code ipaddress.ip_address}: IPv4 uses four decimal octets without
 * leading zeros, and IPv6 uses RFC 4291 text with an optional trailing IPv4 part. Zone IDs are
 * rejected. An IPv4-mapped IPv6 literal classifies as its IPv4 address.
 */
public final class IpAddressLiterals {
    private IpAddressLiterals() {
    }

    /**
     * Parses an IP literal.
     *
     * @param text the literal text, without brackets
     * @return the 4- or 16-byte address, or {@code null} when the text is not a literal
     */
    public static byte[] parse(final String text) {
        if (text == null || text.isEmpty()) {
            return null;
        }
        if (text.indexOf(':') < 0) {
            return parseIpv4(text);
        }
        return parseIpv6(text);
    }

    /**
     * Returns whether the text is an IP literal.
     *
     * @param text the candidate text
     * @return {@code true} for an IPv4 or IPv6 literal
     */
    public static boolean isLiteral(final String text) {
        return parse(text) != null;
    }

    /**
     * Returns whether a literal is a loopback address.
     *
     * @param text the candidate text
     * @return {@code true} for 127.0.0.0/8 or {@code ::1}, including IPv4-mapped forms
     */
    public static boolean isLoopbackLiteral(final String text) {
        final byte[] address = parse(text);
        return address != null && isLoopback(canonical(address));
    }

    static byte[] canonical(final byte[] address) {
        if (address.length == 16) {
            boolean isMapped = true;
            for (int index = 0; index < 10; index++) {
                if (address[index] != 0) {
                    isMapped = false;
                }
            }
            if (isMapped && (address[10] & 0xff) == 0xff && (address[11] & 0xff) == 0xff) {
                return new byte[] {address[12], address[13], address[14], address[15]};
            }
        }
        return address;
    }

    static boolean isLoopback(final byte[] address) {
        if (address.length == 4) {
            return (address[0] & 0xff) == 127;
        }
        for (int index = 0; index < 15; index++) {
            if (address[index] != 0) {
                return false;
            }
        }
        return address[15] == 1;
    }

    static boolean isInNetwork(final byte[] address, final byte[] network, final int prefixLength) {
        if (address.length != network.length) {
            return false;
        }
        for (int bit = 0; bit < prefixLength; bit++) {
            final int mask = 0x80 >>> (bit % 8);
            if ((address[bit / 8] & mask) != (network[bit / 8] & mask)) {
                return false;
            }
        }
        return true;
    }

    static InetAddress toInetAddress(final byte[] address) {
        try {
            return InetAddress.getByAddress(address);
        } catch (UnknownHostException error) {
            throw new IllegalArgumentException("address length is invalid", error);
        }
    }

    private static byte[] parseIpv4(final String text) {
        final String[] parts = text.split("\\.", -1);
        if (parts.length != 4) {
            return null;
        }
        final byte[] result = new byte[4];
        for (int index = 0; index < 4; index++) {
            final String part = parts[index];
            if (part.isEmpty() || part.length() > 3 || (part.length() > 1 && part.charAt(0) == '0')) {
                return null;
            }
            int value = 0;
            for (int position = 0; position < part.length(); position++) {
                final char character = part.charAt(position);
                if (character < '0' || character > '9') {
                    return null;
                }
                value = value * 10 + (character - '0');
            }
            if (value > 255) {
                return null;
            }
            result[index] = (byte) value;
        }
        return result;
    }

    private static byte[] parseIpv6(final String text) {
        if (text.indexOf('%') >= 0) {
            return null;
        }
        String body = text;
        byte[] trailingIpv4 = null;
        final int lastColon = body.lastIndexOf(':');
        if (body.indexOf('.', lastColon) > 0) {
            trailingIpv4 = parseIpv4(body.substring(lastColon + 1));
            if (trailingIpv4 == null) {
                return null;
            }
            body = body.substring(0, lastColon + 1) + "0:0";
        }
        final int doubleColon = body.indexOf("::");
        if (doubleColon >= 0 && body.indexOf("::", doubleColon + 1) >= 0) {
            return null;
        }
        final String[] head;
        final String[] tail;
        if (doubleColon >= 0) {
            final String left = body.substring(0, doubleColon);
            final String right = body.substring(doubleColon + 2);
            head = left.isEmpty() ? new String[0] : left.split(":", -1);
            tail = right.isEmpty() ? new String[0] : right.split(":", -1);
            if (head.length + tail.length > 7) {
                return null;
            }
        } else {
            head = body.split(":", -1);
            tail = new String[0];
            if (head.length != 8) {
                return null;
            }
        }
        final int[] groups = new int[8];
        for (int index = 0; index < head.length; index++) {
            final int value = parseHexGroup(head[index]);
            if (value < 0) {
                return null;
            }
            groups[index] = value;
        }
        for (int index = 0; index < tail.length; index++) {
            final int value = parseHexGroup(tail[index]);
            if (value < 0) {
                return null;
            }
            groups[8 - tail.length + index] = value;
        }
        final byte[] result = new byte[16];
        for (int index = 0; index < 8; index++) {
            result[index * 2] = (byte) (groups[index] >>> 8);
            result[index * 2 + 1] = (byte) groups[index];
        }
        if (trailingIpv4 != null) {
            System.arraycopy(trailingIpv4, 0, result, 12, 4);
        }
        return result;
    }

    private static int parseHexGroup(final String group) {
        if (group.isEmpty() || group.length() > 4) {
            return -1;
        }
        int value = 0;
        for (int index = 0; index < group.length(); index++) {
            final int digit = Character.digit(group.charAt(index), 16);
            if (digit < 0 || group.charAt(index) > 'f') {
                return -1;
            }
            value = value * 16 + digit;
        }
        return value;
    }
}
