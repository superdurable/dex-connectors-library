// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.util.Locale;

/**
 * Lenient URL components with Python {@code urllib.parse.urlsplit} semantics.
 *
 * <p>Connector code uses it to validate provider URLs exactly as the Python and TypeScript SDKs
 * do, before a stricter {@link java.net.URI} parse dials them.
 */
public final class SplitUrl {
    private final String scheme;
    private final String netloc;
    private final String path;
    private final String query;
    private final String fragment;

    private SplitUrl(
            final String scheme, final String netloc, final String path, final String query, final String fragment) {
        this.scheme = scheme;
        this.netloc = netloc;
        this.path = path;
        this.query = query;
        this.fragment = fragment;
    }

    /**
     * Splits a URL into scheme, network location, path, query, and fragment.
     *
     * @param url the URL text
     * @return the components; missing parts are empty
     */
    public static SplitUrl split(final String url) {
        String rest = url;
        String scheme = "";
        final int colon = rest.indexOf(':');
        if (colon > 0 && isScheme(rest.substring(0, colon))) {
            scheme = rest.substring(0, colon).toLowerCase(Locale.ROOT);
            rest = rest.substring(colon + 1);
        }
        String netloc = "";
        if (rest.startsWith("//")) {
            rest = rest.substring(2);
            int end = rest.length();
            for (char delimiter : new char[] {'/', '?', '#'}) {
                final int index = rest.indexOf(delimiter);
                if (index >= 0 && index < end) {
                    end = index;
                }
            }
            netloc = rest.substring(0, end);
            rest = rest.substring(end);
        }
        String fragment = "";
        final int hash = rest.indexOf('#');
        if (hash >= 0) {
            fragment = rest.substring(hash + 1);
            rest = rest.substring(0, hash);
        }
        String query = "";
        final int question = rest.indexOf('?');
        if (question >= 0) {
            query = rest.substring(question + 1);
            rest = rest.substring(0, question);
        }
        return new SplitUrl(scheme, netloc, rest, query, fragment);
    }

    /**
     * Returns the lowercase scheme.
     *
     * @return the scheme, or empty
     */
    public String getScheme() {
        return scheme;
    }

    /**
     * Returns the network location, including user information and port.
     *
     * @return the netloc, or empty
     */
    public String getNetloc() {
        return netloc;
    }

    /**
     * Returns the path.
     *
     * @return the path, or empty
     */
    public String getPath() {
        return path;
    }

    /**
     * Returns the query without {@code ?}.
     *
     * @return the query, or empty
     */
    public String getQuery() {
        return query;
    }

    /**
     * Returns the fragment without {@code #}.
     *
     * @return the fragment, or empty
     */
    public String getFragment() {
        return fragment;
    }

    /**
     * Reports whether the network location carries user information.
     *
     * @return {@code true} when the netloc contains {@code @}
     */
    public boolean hasUserInfo() {
        return netloc.indexOf('@') >= 0;
    }

    /**
     * Returns the lowercase hostname without brackets, user information, or port.
     *
     * @return the hostname, or empty
     */
    public String getHostname() {
        final String host = hostAndPort();
        if (host.startsWith("[")) {
            final int close = host.indexOf(']');
            return close < 0 ? "" : host.substring(1, close).toLowerCase(Locale.ROOT);
        }
        final int colon = host.indexOf(':');
        return (colon >= 0 ? host.substring(0, colon) : host).toLowerCase(Locale.ROOT);
    }

    /**
     * Returns the explicit port.
     *
     * @return the port, or {@code null} when absent
     * @throws IllegalArgumentException if the port is not an integer within 0..65535
     */
    public Integer getPort() {
        final String host = hostAndPort();
        final int start = host.startsWith("[") ? host.indexOf(']') + 1 : 0;
        final int colon = host.indexOf(':', start);
        if (colon < 0) {
            return null;
        }
        final String text = host.substring(colon + 1);
        if (text.isEmpty()) {
            return null;
        }
        for (int index = 0; index < text.length(); index++) {
            if (text.charAt(index) < '0' || text.charAt(index) > '9') {
                throw new IllegalArgumentException("port is invalid");
            }
        }
        if (text.length() > 5 || Integer.parseInt(text) > 65535) {
            throw new IllegalArgumentException("port is out of range");
        }
        return Integer.parseInt(text);
    }

    private String hostAndPort() {
        final int at = netloc.lastIndexOf('@');
        return at >= 0 ? netloc.substring(at + 1) : netloc;
    }

    private static boolean isScheme(final String candidate) {
        if (candidate.isEmpty() || !isAsciiLetter(candidate.charAt(0))) {
            return false;
        }
        for (int index = 1; index < candidate.length(); index++) {
            final char character = candidate.charAt(index);
            if (!isAsciiLetter(character) && !(character >= '0' && character <= '9')
                    && character != '+' && character != '-' && character != '.') {
                return false;
            }
        }
        return true;
    }

    private static boolean isAsciiLetter(final char character) {
        return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z');
    }
}
