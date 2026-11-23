// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dex;

import com.fasterxml.jackson.databind.ObjectMapper;

/** Encodes a value through the Dex Java SDK's own Step output and Attribute value mapper. */
public final class ProjectConfigDexOutputProbe {
    private ProjectConfigDexOutputProbe() {
    }

    /**
     * Encodes a value as a Dex Worker encodes Step outputs with its default mapper.
     *
     * @param value the value
     */
    public static void encodeAsDexValue(final Object value) {
        new ValueMapper(new ObjectMapper().findAndRegisterModules()).encode(value);
    }
}
