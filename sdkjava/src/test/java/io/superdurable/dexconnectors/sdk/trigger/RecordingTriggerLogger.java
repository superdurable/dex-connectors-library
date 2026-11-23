// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** A test logger that keeps every record. */
public final class RecordingTriggerLogger implements TriggerLogger {
    private final List<Record> records = Collections.synchronizedList(new ArrayList<>());

    @Override
    public boolean isEnabledFor(final TriggerLogLevel level) {
        return true;
    }

    @Override
    public void log(final TriggerLogLevel level, final String message, final Map<String, Object> attributes) {
        records.add(new Record(level, message, new LinkedHashMap<>(attributes)));
    }

    /**
     * Returns every record.
     *
     * @return the records
     */
    public List<Record> records() {
        synchronized (records) {
            return new ArrayList<>(records);
        }
    }

    /** One logged record. */
    public static final class Record {
        /** The level. */
        public final TriggerLogLevel level;
        /** The message. */
        public final String message;
        /** The attributes. */
        public final Map<String, Object> attributes;

        Record(final TriggerLogLevel level, final String message, final Map<String, Object> attributes) {
            this.level = level;
            this.message = message;
            this.attributes = attributes;
        }
    }
}
