// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import com.fasterxml.jackson.core.JsonParser;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Iterator;
import java.util.Set;

/**
 * Loads strict JSON test fixtures for connector mocks.
 */
public final class ConnectorMockFixtures {
    private static final ObjectMapper STRICT_MAPPER = new ObjectMapper()
            .enable(JsonParser.Feature.STRICT_DUPLICATE_DETECTION);

    private ConnectorMockFixtures() {
    }

    /**
     * Loads one JSON object, rejecting duplicate fields and unknown top-level fields.
     *
     * @param path the fixture file
     * @param allowedFields the permitted top-level fields, or {@code null} to allow any
     * @return the fixture object
     * @throws IOException if the file cannot be read
     * @throws IllegalArgumentException if the document is invalid
     */
    public static ObjectNode loadFixture(final Path path, final Set<String> allowedFields) throws IOException {
        final JsonNode document;
        try (InputStream source = Files.newInputStream(path)) {
            document = STRICT_MAPPER.readTree(source);
        } catch (JsonProcessingException error) {
            throw new IllegalArgumentException("fixture is not strict JSON", error);
        }
        if (document == null || !document.isObject()) {
            throw new IllegalArgumentException("fixture must be a JSON object");
        }
        if (allowedFields != null) {
            final Iterator<String> names = document.fieldNames();
            while (names.hasNext()) {
                if (!allowedFields.contains(names.next())) {
                    throw new IllegalArgumentException("fixture has unknown fields");
                }
            }
        }
        return (ObjectNode) document;
    }
}
