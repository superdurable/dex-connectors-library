// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.singledispatch;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.load;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import io.superdurable.dexconnectors.sdk.Call;
import io.superdurable.dexconnectors.sdk.CallId;
import io.superdurable.dexconnectors.sdk.ConformanceFixtures.FakeContext;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.OperationRef;
import java.time.Duration;
import java.util.ArrayList;
import java.util.EnumMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class SingleDispatchConformanceTest {
    private static final Map<DecisionKind, String> DECISION_NAMES = new EnumMap<>(DecisionKind.class);

    static {
        DECISION_NAMES.put(DecisionKind.SEND_ALLOWED, "sendAllowed");
        DECISION_NAMES.put(DecisionKind.READ_BACK_REQUIRED, "readBackRequired");
        DECISION_NAMES.put(DecisionKind.FAIL_CLOSED, "failClosed");
        DECISION_NAMES.put(DecisionKind.READ_BACK_THEN_RESEND_ALLOWED, "readBackThenResendAllowed");
    }

    @Test
    void roundTripsMarkersWithGoOmissions() throws Exception {
        final JsonNode markers = load("single-dispatch-markers").get("markers");
        assertEquals(8, markers.size());
        for (JsonNode marker : markers) {
            assertEquals(MAPPER.writeValueAsString(marker), MAPPER.writeValueAsString(Marker.fromJson(marker).toJson()));
        }
        assertThrows(IllegalArgumentException.class, () -> Marker.fromJson(MAPPER.readTree(
                "{\"callId\":\"5af8fafa-06ff-5b2d-bb13-04693809fabf\",\"attempt\":true,\"state\":\"claimed\"}")));
        assertThrows(IllegalArgumentException.class, () -> Marker.fromJson(MAPPER.readTree(
                "{\"callId\":\"5af8fafa-06ff-5b2d-bb13-04693809fabf\",\"attempt\":1.5,\"state\":\"claimed\"}")));
        assertThrows(IllegalArgumentException.class, () -> Marker.fromJson(MAPPER.readTree(
                "{\"callId\":\"5af8fafa-06ff-5b2d-bb13-04693809fabf\",\"attempt\":4294967297,\"state\":\"claimed\"}")));
        final Marker mathematicalIntegers = Marker.fromJson(MAPPER.readTree(
                "{\"callId\":\"5af8fafa-06ff-5b2d-bb13-04693809fabf\",\"attempt\":1.0,\"state\":\"notSent\","
                        + "\"sendCount\":0.0}"));
        assertEquals(1, mathematicalIntegers.getAttempt());
        assertEquals(0, mathematicalIntegers.getSendCount());
        assertEquals(DecisionKind.SEND_ALLOWED, DispatchDecision.decideDispatch(mathematicalIntegers.getCallId(), 2,
                MAPPER.readTree("{\"callId\":\"5af8fafa-06ff-5b2d-bb13-04693809fabf\",\"attempt\":1.0,"
                        + "\"state\":\"notSent\",\"sendCount\":0.0}"), 1).getKind());
    }

    @Test
    void replaysCheckpointScenariosByteForByte() throws Exception {
        final JsonNode scenarios = load("single-dispatch-markers").get("checkpointScenarios");
        assertTrue(scenarios.size() >= 10);
        for (JsonNode scenario : scenarios) {
            final String name = scenario.get("name").textValue();
            final FakeContext context = new FakeContext("flow-42", "step-execution-7",
                    scenario.get("attempt").intValue(), scenario.get("previousHeartbeat"));
            for (JsonNode scenarioCall : scenario.get("calls")) {
                final Call call = Call.create(CallId.of(scenarioCall.get("callId").textValue()),
                        new ConnectionRef("sample", "main"), new OperationRef("sample", "createItem"),
                        context.context(), scenarioCall.get("maxSends").intValue());
                final Fence fence = new Fence(call, Duration.ofSeconds(30),
                        Fence.DEFAULT_WRITE_WINDOW_FRACTION, scenarioCall.get("maxSends").intValue());
                final DispatchDecision decision = fence.decide();
                assertEquals(scenarioCall.get("decision").textValue(), DECISION_NAMES.get(decision.getKind()), name);
                assertEquals(scenarioCall.get("maximumPriorSends").intValue(), decision.getMaximumPriorSends(), name);
                final String decisionProviderId = decision.getMarker() == null ? "" : decision.getMarker().getProviderId();
                assertEquals(scenarioCall.has("decisionProviderId")
                        ? scenarioCall.get("decisionProviderId").textValue() : "", decisionProviderId, name);
                if (scenarioCall.get("dispatch").booleanValue()
                        && (decision.getKind() == DecisionKind.SEND_ALLOWED
                        || decision.getKind() == DecisionKind.READ_BACK_THEN_RESEND_ALLOWED)) {
                    fence.dispatch(writeDeadline -> "sent");
                    if (scenarioCall.has("providerId") && !scenarioCall.get("providerId").textValue().isEmpty()) {
                        fence.recordProviderId(scenarioCall.get("providerId").textValue());
                    }
                }
            }
            final List<String> expected = new ArrayList<>();
            for (JsonNode heartbeat : scenario.get("heartbeats")) {
                expected.add(MAPPER.writeValueAsString(heartbeat));
            }
            assertEquals(expected, context.heartbeats(), name);
        }
    }
}
