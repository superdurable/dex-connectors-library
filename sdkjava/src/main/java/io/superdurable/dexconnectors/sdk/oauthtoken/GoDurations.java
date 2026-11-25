// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.oauthtoken;

import java.math.BigInteger;
import java.time.Duration;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Parses Go {@code time.Duration} text as manifest grant lifetimes and timeouts declare it. */
final class GoDurations {
    private static final Pattern PART = Pattern.compile("((?:[0-9]+(?:\\.[0-9]*)?|\\.[0-9]+))(ns|us|µs|μs|ms|s|m|h)");
    private static final BigInteger MAXIMUM_POSITIVE = BigInteger.valueOf(Long.MAX_VALUE);
    private static final BigInteger MAXIMUM_NEGATIVE = BigInteger.valueOf(Long.MAX_VALUE).add(BigInteger.ONE);

    private GoDurations() {
    }

    static Duration parse(final String text) {
        if (text == null || text.isEmpty() || "0".equals(text) || "+0".equals(text) || "-0".equals(text)) {
            return Duration.ZERO;
        }
        final boolean isNegative = text.startsWith("-");
        final String value = text.startsWith("-") || text.startsWith("+") ? text.substring(1) : text;
        BigInteger total = BigInteger.ZERO;
        int position = 0;
        final Matcher matcher = PART.matcher(value);
        while (matcher.find()) {
            if (matcher.start() != position) {
                throw new IllegalArgumentException("manifest grant duration is invalid");
            }
            final String number = matcher.group(1);
            final int dot = number.indexOf('.');
            final String whole = dot < 0 ? number : number.substring(0, dot);
            final String fraction = dot < 0 ? "" : number.substring(dot + 1);
            final BigInteger unit = BigInteger.valueOf(unitNanoseconds(matcher.group(2)));
            total = total.add(new BigInteger(whole.isEmpty() ? "0" : whole).multiply(unit));
            if (!fraction.isEmpty()) {
                total = total.add(new BigInteger(fraction).multiply(unit).divide(BigInteger.TEN.pow(fraction.length())));
            }
            position = matcher.end();
        }
        final BigInteger maximum = isNegative ? MAXIMUM_NEGATIVE : MAXIMUM_POSITIVE;
        if (position != value.length() || position == 0 || total.compareTo(maximum) > 0) {
            throw new IllegalArgumentException("manifest grant duration is invalid");
        }
        return Duration.ofNanos(0).plusNanos(isNegative ? total.negate().longValue() : total.longValue());
    }

    private static long unitNanoseconds(final String unit) {
        switch (unit) {
            case "ns":
                return 1L;
            case "ms":
                return 1_000_000L;
            case "s":
                return 1_000_000_000L;
            case "m":
                return 60_000_000_000L;
            case "h":
                return 3_600_000_000_000L;
            default:
                return 1_000L;
        }
    }
}
