// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.annotation.JsonCreator;
import com.fasterxml.jackson.annotation.JsonValue;
import java.time.DateTimeException;
import java.time.Instant;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Offset-preserving Go {@code time.Time} JSON value with up to nanosecond precision.
 *
 * <p>The canonical wire form is Go's RFC 3339 Nano representation: a non-UTC numeric offset is
 * preserved, UTC is written as {@code Z}, and trailing fractional zeroes are trimmed. The Go zero
 * time is {@code 0001-01-01T00:00:00Z}. Instances are immutable and compare by wire form.
 */
public final class Rfc3339NanoTimestamp {
    /** The Go zero time, {@code 0001-01-01T00:00:00Z}, used for an uncorrelated receipt. */
    public static final Rfc3339NanoTimestamp ZERO = new Rfc3339NanoTimestamp("0001-01-01T00:00:00Z");

    private static final Pattern TIMESTAMP = Pattern.compile(
            "^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:([.,])([0-9]+))?"
                    + "(Z|[+-][0-9]{2}:[0-9]{2})$");

    private final String wire;

    private Rfc3339NanoTimestamp(final String wire) {
        this.wire = wire;
    }

    /**
     * Validates Go {@code time.Time} JSON input without rounding its supplied precision.
     *
     * @param value the RFC 3339 timestamp text
     * @return the canonical timestamp
     * @throws IllegalArgumentException if the text is not a valid Go RFC 3339 time
     */
    @JsonCreator
    public static Rfc3339NanoTimestamp fromWire(final String value) {
        if (value == null) {
            throw new IllegalArgumentException("timestamp must be a Go RFC 3339 time");
        }
        final Matcher match = TIMESTAMP.matcher(value);
        if (!match.matches()) {
            throw new IllegalArgumentException("timestamp must be a Go RFC 3339 time");
        }
        final String year = match.group(1);
        final int month = Integer.parseInt(match.group(2));
        final int day = Integer.parseInt(match.group(3));
        final int hour = Integer.parseInt(match.group(4));
        final int minute = Integer.parseInt(match.group(5));
        final int second = Integer.parseInt(match.group(6));
        final String fraction = match.group(8) == null ? "" : match.group(8);
        final String offset = match.group(9);
        if (month < 1 || month > 12 || day < 1 || day > daysInMonth(Integer.parseInt(year), month)) {
            throw new IllegalArgumentException("timestamp has an invalid date");
        }
        if (hour > 23 || minute > 59 || second > 59) {
            throw new IllegalArgumentException("timestamp has an invalid clock time");
        }
        if (!"Z".equals(offset)
                && (Integer.parseInt(offset.substring(1, 3)) > 24 || Integer.parseInt(offset.substring(4, 6)) > 60)) {
            throw new IllegalArgumentException("timestamp has an invalid timezone offset");
        }
        final String canonicalFraction = trimTrailingZeroes(fraction);
        final String canonicalOffset = "+00:00".equals(offset) || "-00:00".equals(offset) ? "Z" : offset;
        return new Rfc3339NanoTimestamp(year + "-" + match.group(2) + "-" + match.group(3) + "T"
                + twoDigits(hour) + ":" + match.group(5) + ":" + match.group(6)
                + (canonicalFraction.isEmpty() ? "" : "." + canonicalFraction) + canonicalOffset);
    }

    /**
     * Formats a Java date-time without changing its offset.
     *
     * @param value the offset date-time
     * @return the canonical timestamp
     */
    public static Rfc3339NanoTimestamp fromOffsetDateTime(final OffsetDateTime value) {
        if (value == null) {
            throw new IllegalArgumentException("timestamp is required");
        }
        final StringBuilder builder = new StringBuilder(35);
        final int year = value.getYear();
        if (year < 0 || year > 9999) {
            throw new IllegalArgumentException("timestamp year must be within 0000..9999");
        }
        builder.append(String.format("%04d-%02d-%02dT%02d:%02d:%02d", year, value.getMonthValue(),
                value.getDayOfMonth(), value.getHour(), value.getMinute(), value.getSecond()));
        final String fraction = trimTrailingZeroes(String.format("%09d", value.getNano()));
        if (!fraction.isEmpty()) {
            builder.append('.').append(fraction);
        }
        final ZoneOffset offset = value.getOffset();
        if (offset.getTotalSeconds() == 0) {
            builder.append('Z');
        } else {
            final String id = offset.getId();
            if (id.length() != 6) {
                throw new IllegalArgumentException("timestamp offset must be whole minutes");
            }
            builder.append(id);
        }
        return new Rfc3339NanoTimestamp(builder.toString());
    }

    /**
     * Formats an instant in UTC.
     *
     * @param value the instant
     * @return the canonical UTC timestamp
     */
    public static Rfc3339NanoTimestamp fromInstant(final Instant value) {
        return fromOffsetDateTime(OffsetDateTime.ofInstant(value, ZoneOffset.UTC));
    }

    /**
     * Returns the current time in UTC with the system clock's precision.
     *
     * @return the current timestamp
     */
    public static Rfc3339NanoTimestamp now() {
        return fromInstant(Instant.now());
    }

    /**
     * Parses the timestamp as a Java date-time.
     *
     * @return the offset date-time
     * @throws DateTimeException if Java cannot represent the offset, such as one beyond 18 hours
     */
    public OffsetDateTime toOffsetDateTime() {
        return OffsetDateTime.parse(wire);
    }

    /**
     * Reports whether this is the Go zero time.
     *
     * @return {@code true} for {@code 0001-01-01T00:00:00Z} at any equivalent offset
     */
    public boolean isZero() {
        try {
            return toOffsetDateTime().toInstant().equals(ZERO.toOffsetDateTime().toInstant());
        } catch (DateTimeException error) {
            return false;
        }
    }

    /**
     * Returns the canonical wire representation.
     *
     * @return the RFC 3339 Nano text
     */
    @JsonValue
    public String toWire() {
        return wire;
    }

    @Override
    public String toString() {
        return wire;
    }

    @Override
    public boolean equals(final Object other) {
        return other instanceof Rfc3339NanoTimestamp && wire.equals(((Rfc3339NanoTimestamp) other).wire);
    }

    @Override
    public int hashCode() {
        return wire.hashCode();
    }

    private static int daysInMonth(final int year, final int month) {
        if (month == 2) {
            final boolean isLeapYear = (year % 4 == 0 && year % 100 != 0) || year % 400 == 0;
            return isLeapYear ? 29 : 28;
        }
        return month == 4 || month == 6 || month == 9 || month == 11 ? 30 : 31;
    }

    private static String trimTrailingZeroes(final String fraction) {
        int end = fraction.length();
        while (end > 0 && fraction.charAt(end - 1) == '0') {
            end--;
        }
        return fraction.substring(0, end);
    }

    private static String twoDigits(final int value) {
        return value < 10 ? "0" + value : Integer.toString(value);
    }
}
