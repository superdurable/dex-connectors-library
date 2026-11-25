// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.util.Map;
import java.util.TreeMap;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * The default Trigger logger: writes records through {@code java.util.logging}.
 *
 * <p>Without a logger it uses {@code Logger.getLogger("io.superdurable.dexconnectors.sdk.trigger")}.
 * The SDK never configures levels or handlers. Each record's message is the Trigger message
 * followed by its attributes as compact JSON with sorted keys. Levels map to {@code FINE},
 * {@code INFO}, {@code WARNING}, and {@code SEVERE}.
 */
public final class StandardTriggerLogger implements TriggerLogger {
    /** The logger name used when none is supplied. */
    public static final String LOGGER_NAME = "io.superdurable.dexconnectors.sdk.trigger";

    private static final ObjectMapper ATTRIBUTE_MAPPER = new ObjectMapper();

    private final Logger logger;

    /**
     * Writes to the {@link #LOGGER_NAME} logger.
     */
    public StandardTriggerLogger() {
        this(Logger.getLogger(LOGGER_NAME));
    }

    /**
     * Writes to the given logger.
     *
     * @param logger the destination logger
     */
    public StandardTriggerLogger(final Logger logger) {
        if (logger == null) {
            throw new IllegalArgumentException("logger is required");
        }
        this.logger = logger;
    }

    /** {@inheritDoc} */
    @Override
    public boolean isEnabledFor(final TriggerLogLevel level) {
        return logger.isLoggable(levelOf(level));
    }

    /** {@inheritDoc} */
    @Override
    public void log(final TriggerLogLevel level, final String message, final Map<String, Object> attributes) {
        if (attributes == null || attributes.isEmpty()) {
            logger.log(levelOf(level), message);
            return;
        }
        String rendered;
        try {
            rendered = ATTRIBUTE_MAPPER.writeValueAsString(new TreeMap<>(attributes));
        } catch (JsonProcessingException error) {
            rendered = "{}";
        }
        logger.log(levelOf(level), message + " " + rendered);
    }

    private static Level levelOf(final TriggerLogLevel level) {
        switch (level) {
            case DEBUG:
                return Level.FINE;
            case INFO:
                return Level.INFO;
            case WARN:
                return Level.WARNING;
            default:
                return Level.SEVERE;
        }
    }
}
