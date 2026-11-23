// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.JsonNodeFactory;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * An object read from a Go {@code json.RawMessage} field that remembers its compacted bytes.
 *
 * <p>Go re-encodes such a field by compacting its stored bytes, keeping member order and number
 * spelling. While the tree is unchanged, {@link GoCompatibleJson} writes these bytes; after a
 * change it encodes the tree like any map.
 */
// ObjectNode.deepCopy narrows JsonNode's generic deepCopy, which javac reports as unchecked.
@SuppressWarnings("unchecked")
final class RawMessageObjectNode extends ObjectNode {
    private static final long serialVersionUID = 1L;

    private final String compactedText;

    RawMessageObjectNode(final String compactedText, final ObjectNode value) {
        super(JsonNodeFactory.instance, copyChildren(value));
        this.compactedText = compactedText;
    }

    /** {@inheritDoc} */
    @Override
    public ObjectNode deepCopy() {
        return new RawMessageObjectNode(compactedText, super.deepCopy());
    }

    /** Returns the compacted bytes when the tree still equals them, otherwise {@code null}. */
    String unchangedCompactedText() {
        final JsonNode stored;
        try {
            stored = GoCompatibleJson.parseWithoutRawMessages(compactedText);
        } catch (IllegalArgumentException error) {
            return null;
        }
        return stored instanceof ObjectNode && ((ObjectNode) stored).equals(new ObjectNode(
                JsonNodeFactory.instance, copyChildren(this))) ? compactedText : null;
    }

    private static Map<String, JsonNode> copyChildren(final ObjectNode value) {
        final Map<String, JsonNode> children = new LinkedHashMap<>();
        final Iterator<Map.Entry<String, JsonNode>> fields = value.fields();
        while (fields.hasNext()) {
            final Map.Entry<String, JsonNode> field = fields.next();
            children.put(field.getKey(), field.getValue());
        }
        return children;
    }
}
