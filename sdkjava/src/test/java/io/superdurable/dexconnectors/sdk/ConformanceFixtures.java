// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk;

import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.superdurable.dex.Context;
import java.io.File;
import java.io.IOException;
import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;

/** Loads shared conformance fixtures and builds test Dex Contexts. */
public final class ConformanceFixtures {
    /** The mapper the Dex Java SDK uses by default. */
    public static final ObjectMapper MAPPER = new ObjectMapper().findAndRegisterModules();

    private static final Comparator<JsonNode> NUMERIC_VALUE_COMPARATOR = (left, right) -> {
        if (left.equals(right)) {
            return 0;
        }
        if (left.isNumber() && right.isNumber()) {
            return left.decimalValue().compareTo(right.decimalValue());
        }
        return 1;
    };

    private ConformanceFixtures() {
    }

    /**
     * Returns the shared conformance directory.
     *
     * @return the directory
     */
    public static File directory() {
        final String configured = System.getenv("CONFORMANCE_FIXTURE_DIR");
        return new File(configured != null ? configured : System.getProperty("conformance.directory", "../conformance"));
    }

    /**
     * Reads one fixture.
     *
     * @param name the fixture name without {@code .json}
     * @return the parsed fixture
     * @throws IOException if the file cannot be read
     */
    public static JsonNode load(final String name) throws IOException {
        return MAPPER.readTree(new File(directory(), name + ".json"));
    }

    /**
     * Asserts JSON value equality, treating {@code 2} and {@code 2.0} as equal.
     *
     * @param expected the expected JSON
     * @param actual the actual JSON
     */
    public static void assertJsonValueEquals(final JsonNode expected, final JsonNode actual) {
        assertTrue(expected.equals(NUMERIC_VALUE_COMPARATOR, actual), () -> "expected " + expected + " but was " + actual);
    }

    /**
     * Encodes a value through the Dex mapper and asserts it equals the fixture value.
     *
     * @param expected the fixture value
     * @param value the decoded Java value
     */
    public static void assertEncodes(final JsonNode expected, final Object value) {
        assertJsonValueEquals(expected, MAPPER.valueToTree(value));
    }

    /** A test Step Context that records heartbeats as compact JSON. */
    public static final class FakeContext {
        private final String flowId;
        private final String stepExecutionId;
        private final int attempt;
        private final JsonNode previousHeartbeat;
        private final List<String> heartbeats = new ArrayList<>();
        private final Context context;

        /**
         * Creates a fake Context.
         *
         * @param flowId the Flow ID
         * @param stepExecutionId the Step execution ID
         * @param attempt the attempt
         * @param previousHeartbeat the restored heartbeat, or {@code null}
         */
        public FakeContext(
                final String flowId, final String stepExecutionId, final int attempt, final JsonNode previousHeartbeat) {
            this.flowId = flowId;
            this.stepExecutionId = stepExecutionId;
            this.attempt = attempt;
            this.previousHeartbeat = previousHeartbeat;
            this.context = (Context) Proxy.newProxyInstance(Context.class.getClassLoader(),
                    new Class<?>[] {Context.class}, (proxy, method, arguments) -> {
                        switch (method.getName()) {
                            case "getFlowId":
                                return this.flowId;
                            case "getStepExecutionId":
                                return this.stepExecutionId;
                            case "getAttempt":
                                return this.attempt;
                            case "hasLastHeartbeatValue":
                                return this.previousHeartbeat != null;
                            case "getLastHeartbeatValue":
                                return this.previousHeartbeat == null ? null
                                        : MAPPER.convertValue(this.previousHeartbeat, (Class<?>) arguments[0]);
                            case "recordHeartbeat":
                                heartbeats.add(MAPPER.writeValueAsString(arguments[0]));
                                return null;
                            case "isCancellationRequested":
                                return false;
                            case "hashCode":
                                return System.identityHashCode(proxy);
                            case "equals":
                                return proxy == arguments[0];
                            case "toString":
                                return "FakeContext";
                            default:
                                throw new UnsupportedOperationException(method.getName());
                        }
                    });
        }

        /**
         * Returns the Dex Context proxy.
         *
         * @return the Context
         */
        public Context context() {
            return context;
        }

        /**
         * Returns every heartbeat as compact JSON.
         *
         * @return the heartbeats
         */
        public List<String> heartbeats() {
            return heartbeats;
        }
    }
}
