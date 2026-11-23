// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.connectormock;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.load;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConformanceFixtures.FakeContext;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.ConnectorOperations;
import io.superdurable.dexconnectors.sdk.Failure;
import io.superdurable.dexconnectors.sdk.FailureKind;
import io.superdurable.dexconnectors.sdk.MutationDefinition;
import io.superdurable.dexconnectors.sdk.MutationResult;
import io.superdurable.dexconnectors.sdk.OperationRef;
import io.superdurable.dexconnectors.sdk.QueryDefinition;
import io.superdurable.dexconnectors.sdk.RetryException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.Collections;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class ConnectorMockTest {
    private static final ConnectionRef CONNECTION = new ConnectionRef("sample", "main");
    private static final String CALL_ID = "5af8fafa-06ff-5b2d-bb13-04693809fabf";

    @Test
    void operationAdaptersUseRealDefinitionsAndRequireStepContext() throws Exception {
        final JsonNode fixture = load("connectormock");
        final QueryDefinition queryDefinition = MAPPER.treeToValue(fixture.get("queryDefinition"), QueryDefinition.class);
        final MutationDefinition mutationDefinition =
                MAPPER.treeToValue(fixture.get("mutationDefinition"), MutationDefinition.class);
        final ScriptedQuery<Map<String, String>, String> query =
                new ScriptedQuery<Map<String, String>, String>(queryDefinition).respond(MockCase.branch("found", "ok"));
        final ScriptedMutation<Map<String, String>, String> mutation =
                new ScriptedMutation<Map<String, String>, String>(mutationDefinition)
                        .respond(MockCase.branch("created", "ok"));
        assertEquals(queryDefinition, query.definition());
        assertEquals(mutationDefinition, mutation.definition());
        assertEquals(CALL_ID, mutation.idempotencyKey(CallId.of(CALL_ID), Collections.<String, String>emptyMap()));
        final Call call = Call.create(CallId.of(CALL_ID), CONNECTION, queryDefinition.getOperation(), null, 1);
        final IllegalStateException error = assertThrows(IllegalStateException.class,
                () -> query.invoke(call, Collections.<String, String>emptyMap()));
        assertTrue(error.getMessage().contains("live Dex Step context"));
    }

    @Test
    void scriptedMutationReplaysTerminalResultAcrossDexRetries() throws Exception {
        final MutationDefinition definition =
                MAPPER.treeToValue(load("connectormock").get("mutationDefinition"), MutationDefinition.class);
        final ScriptedMutation<String, String> mutation = new ScriptedMutation<String, String>(definition).respond(
                MockCase.<String>retry(new Failure(FailureKind.RATE_LIMIT, "sample", "createItem", "throttled")),
                MockCase.branch("created", "object-42"));
        final FakeContext first = new FakeContext("flow-42", "step-execution-7", 1, null);
        assertThrows(RetryException.class, () -> ConnectorOperations.runMutation(mutation,
                Call.forStep(first.context(), CONNECTION, definition.getOperation(), 1), "input"));
        final FakeContext second = new FakeContext("flow-42", "step-execution-7", 2, null);
        final MutationResult<String> created = ConnectorOperations.runMutation(mutation,
                Call.forStep(second.context(), CONNECTION, definition.getOperation(), 1), "input");
        assertEquals("created", created.getBranch());
        assertEquals(CALL_ID, created.getReceipt().getIdempotencyKey());
        final FakeContext third = new FakeContext("flow-42", "step-execution-7", 3, null);
        final MutationResult<String> replayed = ConnectorOperations.runMutation(mutation,
                Call.forStep(third.context(), CONNECTION, definition.getOperation(), 1), "input");
        assertEquals("object-42", replayed.getValue());
        assertTrue(mutation.calls().get(2).isReplay());
        assertTrue(mutation.calls().get(0).isRetry());
        mutation.assertNoFailures();
    }

    @Test
    void flowPrecedenceRetryAndTerminalKeyReplay() {
        final ScriptedOperation<Map<String, String>, String> mock = new ScriptedOperation<Map<String, String>, String>(
                new OperationRef("sample", "createItem"), Arrays.asList("created", "defect", "uncertain"), true, null);
        final MockCase<String> retry =
                MockCase.retry(new Failure(FailureKind.RATE_LIMIT, "sample", "createItem", "throttled"));
        mock.defaults(MockCase.branch("created", "default"));
        mock.forFlow("flow-special").respond(retry, MockCase.branch("created", "special"));
        final Map<String, String> input = Collections.emptyMap();
        assertEquals(MockCase.Kind.RETRY, mock.answer("flow-special-1", "call", CONNECTION, input, "key", 1).getKind());
        assertEquals("special", mock.answer("flow-special-1", "call", CONNECTION, input, "key", 1).getValue());
        assertEquals("special", mock.answer("flow-special-1", "call", CONNECTION, input, "key", 1).getValue());
        assertTrue(mock.calls().get(mock.calls().size() - 1).isReplay());
        assertEquals("default", mock.answer("flow-other", "call-2", CONNECTION, input, "other", 1).getValue());
    }

    @Test
    void longestFlowPrefixWinsAndRulesPrecedeQueue() {
        final ScriptedOperation<String, String> mock = new ScriptedOperation<String, String>(
                new OperationRef("sample", "getItem"), Arrays.asList("found", "defect"), false, null);
        mock.forFlow("flow").respond(MockCase.branch("found", "short"));
        mock.forFlow("flow-long").respond(MockCase.branch("found", "long"));
        mock.respond(MockCase.branch("found", "queued")).when("special"::equals, MockCase.branch("found", "rule"));
        assertEquals("long", mock.answer("flow-long-1", "c", CONNECTION, "x", "", 1).getValue());
        assertEquals("short", mock.answer("flow-long-1", "c", CONNECTION, "x", "", 1).getValue());
        assertEquals("rule", mock.answer("other", "c", CONNECTION, "special", "", 1).getValue());
        assertEquals("queued", mock.answer("other", "c", CONNECTION, "x", "", 1).getValue());
    }

    @Test
    void strictFixtureLoaderAndGoRecordedCall(@TempDir final Path directory) throws Exception {
        final JsonNode fixture = load("connectormock");
        final ScriptedOperation<Map<String, String>, String> mock = new ScriptedOperation<Map<String, String>, String>(
                new OperationRef("sample", "createItem"), Arrays.asList("created", "defect"), false, null);
        mock.respond(MockCase.branch("created", "ok"));
        mock.answer("flow-42", fixture.get("recordedCall").get("callId").textValue(), CONNECTION,
                Collections.<String, String>emptyMap(), "", 1);
        assertEquals(fixture.get("recordedCall"), mock.calls().get(0).toJson());
        final Path duplicate = directory.resolve("duplicate.json");
        Files.write(duplicate, "{\"a\":1,\"a\":2}".getBytes(StandardCharsets.UTF_8));
        assertThrows(IllegalArgumentException.class, () -> ConnectorMockFixtures.loadFixture(duplicate, null));
        final Path unknown = directory.resolve("unknown.json");
        Files.write(unknown, "{\"a\":1,\"b\":2}".getBytes(StandardCharsets.UTF_8));
        assertThrows(IllegalArgumentException.class,
                () -> ConnectorMockFixtures.loadFixture(unknown, Collections.singleton("a")));
        assertEquals(1, ConnectorMockFixtures.loadFixture(unknown, null).get("a").intValue());
    }

    @Test
    void uncertainOnlyForDeclaredMutation() {
        final Failure failure = new Failure(FailureKind.TRANSPORT, "sample", "createItem", "outcome unknown");
        final ScriptedOperation<String, String> mock = new ScriptedOperation<String, String>(
                new OperationRef("sample", "getItem"), Arrays.asList("found", "defect"), false, null);
        assertThrows(IllegalArgumentException.class, () -> mock.respond(MockCase.uncertain("", failure)));
    }

    @Test
    void strictUnscriptedAndInvalidBranch() {
        final ScriptedOperation<String, String> mock = new ScriptedOperation<String, String>(
                new OperationRef("sample", "getItem"), Arrays.asList("found", "defect"), false, null);
        mock.defaults(MockCase.branch("found", "fallback")).strict();
        assertEquals("defect", mock.answer("flow-1", "call-1", CONNECTION, "x", "", 1).getBranch());
        assertTrue(mock.calls().get(0).isUnscripted());
        assertFalse(mock.failures().isEmpty());
        assertThrows(IllegalArgumentException.class, () -> mock.respond(MockCase.branch("undeclared", "value")));
    }

    @Test
    void pagedCaseRejectsOutOfRangePage() {
        final ScriptedOperation<Integer, String> mock = new ScriptedOperation<Integer, String>(
                new OperationRef("sample", "listItems"), Arrays.asList("found", "defect"), false, null);
        mock.respond(MockCase.paged("found", "first", "second"));
        assertEquals("defect", mock.answer("flow-1", "call-1", CONNECTION, 3, "", 3).getBranch());
        assertFalse(mock.failures().isEmpty());
        assertThrows(AssertionError.class, mock::assertNoFailures);
        mock.respond(MockCase.paged("found", "first", "second"));
        assertEquals("second", mock.answer("flow-1", "call-2", CONNECTION, 2, "", 2).getValue());
    }

    @Test
    void goReportAndRecordedFlags(@TempDir final Path directory) throws Exception {
        final JsonNode fixture = load("connectormock");
        assertEquals(ConnectorMockReport.REPORT_DIRECTORY_ENVIRONMENT,
                fixture.get("reportDirectoryEnvironment").textValue());
        assertEquals(ConnectorMockReport.REPORT_FILE_NAME, fixture.get("reportFileName").textValue());
        for (String[] keyAndFlag : new String[][] {
            {"retryRecordedCall", "retry"}, {"replayedRecordedCall", "replay"}, {"unscriptedRecordedCall", "unscripted"},
        }) {
            final JsonNode value = fixture.get(keyAndFlag[0]);
            final RecordedCall<Object> recorded = new RecordedCall<Object>(value.get("connectorId").textValue(),
                    value.get("operationId").textValue(), value.get("connectionName").textValue(),
                    value.get("flowId").textValue(), value.get("callId").textValue(), null,
                    value.has("branch") ? value.get("branch").textValue() : "",
                    "retry".equals(keyAndFlag[1]), "replay".equals(keyAndFlag[1]), "unscripted".equals(keyAndFlag[1]));
            assertEquals(value, recorded.toJson());
        }
        RecordedCall<Object> recorded = null;
        for (JsonNode row : fixture.get("reportFile").get("calls")) {
            recorded = new RecordedCall<Object>(row.get("connectorId").textValue(), row.get("operation").textValue(),
                    row.get("connection").textValue(), row.get("flowId").textValue(), row.get("callId").textValue(),
                    null, row.has("branch") ? row.get("branch").textValue() : "", row.path("retry").asBoolean(false),
                    row.path("replay").asBoolean(false), row.path("unscripted").asBoolean(false));
            assertEquals(row, MAPPER.valueToTree(recorded.toReportRow()));
            ConnectorMockReport.appendToReport(recorded, directory);
        }
        final Path report = directory.resolve(fixture.get("reportFileName").textValue());
        final String reportText = new String(Files.readAllBytes(report), StandardCharsets.UTF_8);
        assertTrue(reportText.contains("\\u003c\\u0026\\u003e"));
        assertEquals(fixture.get("reportFile"), MAPPER.readTree(reportText));
        assertEquals(fixture.get("report"), MAPPER.valueToTree(new RecordedCall<Object>("sample", "createItem", "main",
                "flow-42", fixture.get("reportFile").get("calls").get(0).get("callId").textValue(), null, "created",
                false, false, false).toReportRow()));
        Files.write(report, "{\"calls\":[]}\n".getBytes(StandardCharsets.UTF_8));
        final RecordedCall<Object> last = recorded;
        assertThrows(java.io.IOException.class, () -> ConnectorMockReport.appendToReport(last, directory));
    }
}
