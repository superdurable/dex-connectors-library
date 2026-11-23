// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.assertEncodes;
import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.load;
import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.exc.MismatchedInputException;
import com.fasterxml.jackson.databind.exc.ValueInstantiationException;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Iterator;
import java.util.Map;
import org.junit.jupiter.api.Test;

class CoreConformanceTest {
    @Test
    void readsEveryCommittedJsonFixture() throws Exception {
        final String[] names = ConformanceFixtures.directory().list((directory, name) -> name.endsWith(".json"));
        Arrays.sort(names);
        assertArrayEquals(new String[] {"connections.json", "connectormock.json", "credential-broker.json",
            "failures.json", "identity.json", "project-environment.json", "project-storage.json", "results.json",
            "retry-policy.json", "single-dispatch-markers.json"}, names);
        for (String name : names) {
            final JsonNode value = MAPPER.readTree(new File(ConformanceFixtures.directory(), name));
            assertEquals(value, MAPPER.readTree(MAPPER.writeValueAsString(value)));
        }
    }

    @Test
    void derivesStableIdentity() throws Exception {
        final JsonNode identity = load("identity");
        final JsonNode source = identity.get("source");
        final Call derivedCall = MAPPER.treeToValue(identity.get("derivedCall"), Call.class);
        assertEncodes(identity.get("derivedCall"), derivedCall);
        final OperationRef operation = derivedCall.getOperation();
        assertEquals(identity.get("callIDNamespace").textValue(), CallId.NAMESPACE.toString());
        assertEquals(derivedCall.getId(),
                CallId.derive(source.get("flowId").textValue(), source.get("stepExecutionId").textValue(), operation));
        for (int occurrence = 1; occurrence <= 3; occurrence++) {
            assertEquals(identity.get("occurrenceCallIds").get(occurrence - 1).textValue(),
                    CallId.derive(source.get("flowId").textValue(), source.get("stepExecutionId").textValue(),
                            operation, occurrence).value());
        }
        final ConformanceFixtures.FakeContext stepContext = new ConformanceFixtures.FakeContext(
                source.get("flowId").textValue(), source.get("stepExecutionId").textValue(), 1, null);
        for (int occurrence = 1; occurrence <= 3; occurrence++) {
            assertEquals(identity.get("occurrenceCallIds").get(occurrence - 1).textValue(),
                    Call.forStep(stepContext.context(), derivedCall.getConnection(), operation, 1).getId().value());
        }
        final ConformanceFixtures.FakeContext retriedContext = new ConformanceFixtures.FakeContext(
                source.get("flowId").textValue(), source.get("stepExecutionId").textValue(), 2, null);
        assertEquals(derivedCall.getId(),
                Call.forStep(retriedContext.context(), derivedCall.getConnection(), operation, 1).getId());
        assertThrows(IllegalArgumentException.class,
                () -> CallId.derive(source.get("flowId").textValue(), source.get("stepExecutionId").textValue(),
                        operation, 0));
        assertEquals(identity.get("differentStepCallId").textValue(),
                CallId.derive(source.get("flowId").textValue(), "step-execution-8", operation).value());
        assertEquals(identity.get("differentOperationCallId").textValue(),
                CallId.derive(source.get("flowId").textValue(), source.get("stepExecutionId").textValue(),
                        new OperationRef(source.get("connectorId").textValue(), "getItem")).value());
        final JsonNode unicode = identity.get("unicodeSource");
        assertTrue(unicode.get("flowId").textValue().getBytes(StandardCharsets.UTF_8).length
                > unicode.get("flowId").textValue().length());
        assertEquals(identity.get("unicodeDerivedCallId").textValue(),
                CallId.derive(unicode.get("flowId").textValue(), unicode.get("stepExecutionId").textValue(),
                        new OperationRef(unicode.get("connectorId").textValue(),
                                unicode.get("operationId").textValue())).value());
        assertEquals(identity.get("providerKey").textValue(),
                source.get("connectorId").textValue() + "/" + derivedCall.getId());
        final Receipt receipt = MAPPER.treeToValue(identity.get("fallbackCompletedReceipt"), Receipt.class);
        assertEncodes(identity.get("fallbackCompletedReceipt"), receipt);
        final Receipt completed = ConnectorOperations.completeReceipt(
                Receipt.newBuilder().observedAt(receipt.getObservedAt()).build(),
                derivedCall);
        assertEncodes(identity.get("fallbackCompletedReceipt"), completed);
    }

    @Test
    void roundTripsEveryFailureKind() throws Exception {
        final JsonNode failures = load("failures");
        assertEquals(13, failures.size());
        assertEquals(FailureKind.values().length, failures.size());
        for (JsonNode failure : failures) {
            assertEncodes(failure, MAPPER.treeToValue(failure, Failure.class));
        }
        final JsonNode extra = MAPPER.readTree(
                "{\"kind\":\"CONFLICT\",\"provider\":\"p\",\"operation\":\"o\",\"message\":\"m\",\"extra\":1}");
        assertThrows(MismatchedInputException.class, () -> MAPPER.treeToValue(extra, Failure.class));
        final JsonNode empty = MAPPER.readTree("{\"kind\":\"CONFLICT\",\"provider\":\"\",\"operation\":\"o\",\"message\":\"m\"}");
        assertThrows(ValueInstantiationException.class, () -> MAPPER.treeToValue(empty, Failure.class));
    }

    @Test
    void roundTripsTerminalResultsAndReceipts() throws Exception {
        final JsonNode results = load("results");
        final Iterator<Map.Entry<String, JsonNode>> fields = results.fields();
        while (fields.hasNext()) {
            final Map.Entry<String, JsonNode> entry = fields.next();
            final JsonNode value = entry.getValue();
            if (entry.getKey().startsWith("query")) {
                assertEncodes(value, MAPPER.convertValue(value, new TypeReference<QueryResult<Object>>() { }));
            } else if (entry.getKey().startsWith("mutation")) {
                assertEncodes(value, MAPPER.convertValue(value, new TypeReference<MutationResult<Object>>() { }));
            } else {
                assertEncodes(value, MAPPER.treeToValue(value, Receipt.class));
            }
        }
        final QueryResult<Object> queryNull = MAPPER.convertValue(results.get("queryNull"),
                new TypeReference<QueryResult<Object>>() { });
        assertNull(queryNull.getValue());
        assertTrue(MAPPER.valueToTree(queryNull).has("value"));
        assertTrue(queryNull.getReceipt().getObservedAt().isZero());
        assertEquals("2026-10-29T12:30:45.123456789+08:00",
                MAPPER.treeToValue(results.get("nanosecondOffsetReceipt"), Receipt.class).getObservedAt().toWire());
        assertEquals("{\"label\":\"élément <&>   😀\"}",
                MAPPER.writeValueAsString(results.get("nanosecondOffsetReceipt").get("metadata")));
    }

    @Test
    void encodesDexRetryNanosecondsExactly() throws Exception {
        final JsonNode expected = load("retry-policy");
        final ConnectorRetryPolicy policy = ConnectorRetryPolicy.defaultConnectorStepExecuteRetryPolicy();
        assertEquals(MAPPER.writeValueAsString(expected), MAPPER.writeValueAsString(policy));
        assertEquals(policy, MAPPER.treeToValue(expected, ConnectorRetryPolicy.class));
    }

    @Test
    void roundTripsOperationDefinitions() throws Exception {
        final JsonNode mock = load("connectormock");
        final QueryDefinition query = MAPPER.treeToValue(mock.get("queryDefinition"), QueryDefinition.class);
        query.validate();
        assertEncodes(mock.get("queryDefinition"), query);
        final MutationDefinition mutation = MAPPER.treeToValue(mock.get("mutationDefinition"), MutationDefinition.class);
        mutation.validate();
        assertEncodes(mock.get("mutationDefinition"), mutation);
        final QueryDefinition empty = MAPPER.treeToValue(mock.get("emptyBranches"), QueryDefinition.class);
        assertEquals(0, empty.getBranches().size());
        assertEncodes(mock.get("emptyBranches"), empty);
        final QueryDefinition none = MAPPER.treeToValue(mock.get("nullBranches"), QueryDefinition.class);
        assertNull(none.getBranches());
        assertEncodes(mock.get("nullBranches"), none);
        assertEquals("{\"executeMethodTimeout\":15000000000,\"executeRetry\":{\"InitialInterval\":1000000000,"
                + "\"BackoffCoefficient\":2,\"MaximumInterval\":300000000000,\"MaximumAttempts\":0,"
                + "\"TotalDuration\":3600000000000},\"executeDurability\":2}",
                MAPPER.writeValueAsString(mutation.getStepDefaults()));
    }

    @Test
    void canonicalizesGoTimestamps() {
        assertEquals("2026-10-29T12:30:45Z", Rfc3339NanoTimestamp.fromWire("2026-10-29T12:30:45.000+00:00").toWire());
        assertEquals("2026-10-29T12:30:45.1-07:00", Rfc3339NanoTimestamp.fromWire("2026-10-29T12:30:45.100-07:00").toWire());
        assertEquals("0001-01-01T00:00:00Z", Rfc3339NanoTimestamp.ZERO.toWire());
        assertEquals("2026-10-29T12:30:45.123456789Z",
                Rfc3339NanoTimestamp.fromInstant(java.time.Instant.parse("2026-10-29T12:30:45.123456789Z")).toWire());
        assertThrows(IllegalArgumentException.class, () -> Rfc3339NanoTimestamp.fromWire("2026-02-30T00:00:00Z"));
        assertThrows(IllegalArgumentException.class, () -> Rfc3339NanoTimestamp.fromWire("2026-10-29 12:30:45Z"));
    }
}
