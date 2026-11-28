// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.rippling;

import com.fasterxml.jackson.databind.*;
import com.fasterxml.jackson.databind.cfg.*;
import com.fasterxml.jackson.databind.type.LogicalType;

import java.io.IOException;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.regex.Pattern;

final class WorkersQueries {
    private static final Pattern WORKER_ID = Pattern.compile("[a-zA-Z0-9_-]{1,64}");
    private static final ObjectMapper WIRE =
            RipplingClient.JSON
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

    static final class Get extends RipplingClient.WorkerQuery<GetWorkerInput, WorkerSummary> {
        Get(RipplingClient client) {
            super(client, "getWorker", new WorkerSummary("", "", ""));
        }

        boolean containsCredential(WorkerSummary output, String token) {
            return workerContainsCredential(output, token);
        }

        String suffix(GetWorkerInput input) {
            if (input == null || !WORKER_ID.matcher(input.workerId).matches())
                throw new IllegalArgumentException("Invalid worker ID");
            return input.workerId + "/";
        }

        WorkerSummary decode(byte[] body, GetWorkerInput input) throws IOException {
            WorkerSummary profile =
                    profile(
                            WIRE.readValue(
                                    new String(body, StandardCharsets.UTF_8), WireWorker.class));
            if (!input.workerId.equals(profile.id)) throw new IOException("Mismatched worker ID");
            return profile;
        }
    }

    static final class ListPage extends RipplingClient.WorkerQuery<ListWorkersInput, WorkerPage> {
        ListPage(RipplingClient client) {
            super(client, "listWorkers", new WorkerPage(Collections.emptyList(), ""));
        }

        boolean containsCredential(WorkerPage output, String token) {
            return output.nextCursor.contains(token)
                    || output.workers.stream()
                            .anyMatch(worker -> workerContainsCredential(worker, token));
        }

        String suffix(ListWorkersInput input) throws IOException {
            if (input == null
                    || input.limit < 0
                    || input.limit > 100
                    || input.cursor.getBytes(StandardCharsets.UTF_8).length > 2048)
                throw new IllegalArgumentException("Invalid Rippling page options");
            StringJoiner query = new StringJoiner("&", "?", "").setEmptyValue("");
            append(query, "cursor", input.cursor);
            if (input.limit > 0) append(query, "limit", Long.toString(input.limit));
            return query.toString();
        }

        WorkerPage decode(byte[] body, ListWorkersInput input) throws IOException {
            WirePage page =
                    WIRE.readValue(new String(body, StandardCharsets.UTF_8), WirePage.class);
            if (page == null || page.results == null) throw new IOException("Invalid worker page");
            List<WorkerSummary> workers = new ArrayList<>(page.results.size());
            for (WireWorker row : page.results) workers.add(profile(row));
            return new WorkerPage(workers, nextCursor(page.next_link));
        }
    }

    private static String nextCursor(String link) throws IOException {
        if (link == null || link.isEmpty()) return "";
        if (link.chars().anyMatch(c -> c < 32 || c == 127))
            throw new IOException("Invalid worker page link");
        String[] locationParts = link.split("#", 2);
        if (locationParts.length == 2) URLDecoder.decode(locationParts[1], "UTF-8");
        String location = locationParts[0];
        int queryStart = location.indexOf('?');
        if (queryStart < 0) throw new IOException("Missing page cursor");
        URI.create(location.substring(0, queryStart).replace(" ", "%20"));
        for (String pair : location.substring(queryStart + 1).split("&")) {
            if (pair.indexOf(';') >= 0) continue;
            String[] fields = pair.split("=", 2);
            try {
                String key = URLDecoder.decode(fields[0], "UTF-8");
                String value = fields.length == 1 ? "" : URLDecoder.decode(fields[1], "UTF-8");
                if (key.equals("cursor")) {
                    if (value.isEmpty()) break;
                    return value;
                }
            } catch (IllegalArgumentException malformedEscape) {
                continue;
            }
        }
        throw new IOException("Invalid worker page cursor");
    }

    private static void append(StringJoiner query, String key, String value) throws IOException {
        if (!value.isEmpty()) query.add(key + "=" + URLEncoder.encode(value, "UTF-8"));
    }

    private static boolean workerContainsCredential(WorkerSummary worker, String token) {
        return worker.id.contains(token)
                || worker.title.contains(token)
                || worker.status.contains(token);
    }

    private static WorkerSummary profile(WireWorker worker) throws IOException {
        if (worker == null || worker.id == null || !WORKER_ID.matcher(worker.id).matches())
            throw new IOException("Invalid worker ID");
        return new WorkerSummary(worker.id, worker.title, worker.status);
    }

    private static final class WireWorker {
        public String id;
        public String title;
        public String status;
    }

    private static final class WirePage {
        public List<WireWorker> results;
        public String next_link;
    }
}
