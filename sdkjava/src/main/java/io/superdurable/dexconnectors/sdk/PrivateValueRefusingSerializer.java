// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import com.fasterxml.jackson.core.JsonGenerator;
import com.fasterxml.jackson.databind.JsonMappingException;
import com.fasterxml.jackson.databind.SerializerProvider;
import com.fasterxml.jackson.databind.ser.std.StdSerializer;
import java.io.IOException;

/**
 * Jackson serializer that refuses to encode private credential values.
 *
 * <p>Types such as {@link SecretString} and project credential material carry it, as Go's
 * {@code MarshalJSON} refuses them, so a value that reaches Flow state, a result, or a structured
 * log fails instead of revealing secrets. The error names only the type, never the contents.
 * Storage and HTTP boundaries encode these values explicitly.
 */
public final class PrivateValueRefusingSerializer extends StdSerializer<Object> {
    private static final long serialVersionUID = 1L;

    /**
     * Creates the serializer; Jackson instantiates it from {@code @JsonSerialize}.
     */
    public PrivateValueRefusingSerializer() {
        super(Object.class);
    }

    /**
     * Refuses to encode the value.
     *
     * @param value the private value
     * @param generator the JSON generator
     * @param provider the serializer provider
     * @throws IOException always, without the value's contents
     */
    @Override
    public void serialize(final Object value, final JsonGenerator generator, final SerializerProvider provider)
            throws IOException {
        throw JsonMappingException.from(generator,
                "connector private value " + value.getClass().getSimpleName() + " cannot be serialized");
    }
}
