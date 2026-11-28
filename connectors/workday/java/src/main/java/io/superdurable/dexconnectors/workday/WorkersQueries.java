// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.workday;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.cfg.*;
import com.fasterxml.jackson.databind.type.LogicalType;

import java.io.IOException;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.regex.Pattern;

final class WorkersQueries {
    private static final Pattern WORKER_ID = Pattern.compile("[A-Fa-f0-9]{32}");
    private static final ObjectMapper WIRE =
            WorkdayClient.JSON
                    .copy()
                    .enable(MapperFeature.ACCEPT_CASE_INSENSITIVE_PROPERTIES)
                    .disable(
                            DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES,
                            DeserializationFeature.ACCEPT_FLOAT_AS_INT);

    static {
        WIRE.coercionConfigFor(LogicalType.Integer)
                .setCoercion(CoercionInputShape.String, CoercionAction.Fail)
                .setCoercion(CoercionInputShape.Boolean, CoercionAction.Fail);
        MutableCoercionConfig strings = WIRE.coercionConfigFor(LogicalType.Textual);
        for (CoercionInputShape shape :
                Arrays.asList(
                        CoercionInputShape.Integer,
                        CoercionInputShape.Float,
                        CoercionInputShape.Boolean))
            strings.setCoercion(shape, CoercionAction.Fail);
    }

    private WorkersQueries() {}

    static final class Get extends WorkdayClient.WorkerQuery<GetWorkerInput, WorkerSummary> {
        Get(WorkdayClient client) {
            super(client, "getWorker", new WorkerSummary("", ""));
        }

        String successBranch() {
            return "found";
        }

        String suffix(GetWorkerInput input) {
            if (input == null || !WORKER_ID.matcher(input.workerId).matches())
                throw new IllegalArgumentException("Invalid worker WID");
            return "/" + input.workerId;
        }

        WorkerSummary decode(byte[] body, GetWorkerInput input) throws IOException {
            WorkerSummary profile =
                    profile(
                            WIRE.readValue(
                                    new String(body, StandardCharsets.UTF_8), WireWorker.class));
            if (!input.workerId.equals(profile.id)) throw new IOException("Mismatched worker WID");
            return profile;
        }
    }

    static final class ListPage extends WorkdayClient.WorkerQuery<ListWorkersInput, WorkerPage> {
        ListPage(WorkdayClient client) {
            super(client, "listWorkers", new WorkerPage(0, Collections.emptyList()));
        }

        String successBranch() {
            return "found";
        }

        String suffix(ListWorkersInput input) throws IOException {
            if (input == null
                    || input.limit < 0
                    || input.limit > 100
                    || input.offset < 0
                    || (!input.search.isEmpty()
                            && input.search
                                            .replaceAll("(?U)^\\s+|\\s+$", "")
                                            .getBytes(StandardCharsets.UTF_8)
                                            .length
                                    < 3))
                throw new IllegalArgumentException("Invalid Workday page options");
            StringJoiner query = new StringJoiner("&", "?", "").setEmptyValue("");
            if (input.limit > 0) append(query, "limit", Long.toString(input.limit));
            if (input.offset > 0) append(query, "offset", Long.toString(input.offset));
            append(query, "search", input.search);
            return query.toString();
        }

        WorkerPage decode(byte[] body, ListWorkersInput input) throws IOException {
            WirePage page =
                    WIRE.readValue(new String(body, StandardCharsets.UTF_8), WirePage.class);
            if (page == null || page.data == null || page.total < 0)
                throw new IOException("Invalid worker page");
            List<WorkerSummary> workers = new ArrayList<>(page.data.size());
            for (WireWorker worker : page.data) workers.add(profile(worker));
            return new WorkerPage(page.total, workers);
        }
    }

    private static void append(StringJoiner query, String key, String value) throws IOException {
        if (!value.isEmpty()) query.add(key + "=" + URLEncoder.encode(value, "UTF-8"));
    }

    private static WorkerSummary profile(WireWorker worker) throws IOException {
        if (worker == null
                || worker.id == null
                || !WORKER_ID.matcher(worker.id).matches()
                || worker.descriptor == null
                || worker.descriptor.isEmpty()) throw new IOException("Incomplete worker summary");
        return new WorkerSummary(worker.id, worker.descriptor);
    }

    private static final class WireWorker {
        public String id;
        public String descriptor;
    }

    private static final class WirePage {
        public long total;
        public List<WireWorker> data;
    }
}
