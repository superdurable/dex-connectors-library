// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import java.nio.charset.StandardCharsets;
import java.util.regex.Pattern;

/** Poll cursor identity rules shared with Trigger inbox keys. */
final class PollCursorNames {
    private static final Pattern CONNECTOR_ID = Pattern.compile("^[a-z][a-z0-9-]{1,62}$");
    private static final Pattern TRIGGER_OR_FEED_NAME = Pattern.compile("^[a-z][A-Za-z0-9]{1,62}$");
    private static final int MAXIMUM_NAME_BYTES = 256;

    private PollCursorNames() {
    }

    static void validate(final String connectorId, final String triggerOrFeedName, final String... names) {
        if (!CONNECTOR_ID.matcher(connectorId).matches() || !TRIGGER_OR_FEED_NAME.matcher(triggerOrFeedName).matches()) {
            throw new IllegalArgumentException("poll cursor connector, trigger, or feed identity is invalid");
        }
        for (String name : names) {
            if (!isValidName(name)) {
                throw new IllegalArgumentException("poll cursor connection or binding name is invalid");
            }
        }
    }

    static boolean isValidName(final String name) {
        return name != null && !name.isEmpty()
                && name.getBytes(StandardCharsets.UTF_8).length <= MAXIMUM_NAME_BYTES
                && name.indexOf('\u0000') < 0 && name.indexOf('\r') < 0 && name.indexOf('\n') < 0
                && !isGoSpace(name.codePointAt(0)) && !isGoSpace(name.codePointBefore(name.length()));
    }

    /** Reports whether Go's {@code strings.TrimSpace} would leave the value empty. */
    static boolean isGoBlank(final String value) {
        int index = 0;
        while (index < value.length()) {
            final int codePoint = value.codePointAt(index);
            if (!isGoSpace(codePoint)) {
                return false;
            }
            index += Character.charCount(codePoint);
        }
        return true;
    }

    private static boolean isGoSpace(final int codePoint) {
        switch (codePoint) {
            case '\t':
            case '\n':
            case 0x0B:
            case '\f':
            case '\r':
            case ' ':
            case 0x85:
            case 0xA0:
                return true;
            default:
                final int type = Character.getType(codePoint);
                return type == Character.SPACE_SEPARATOR || type == Character.LINE_SEPARATOR
                        || type == Character.PARAGRAPH_SEPARATOR;
        }
    }
}
