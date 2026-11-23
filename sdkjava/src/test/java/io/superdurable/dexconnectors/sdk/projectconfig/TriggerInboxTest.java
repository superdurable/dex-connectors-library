// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import static io.superdurable.dexconnectors.sdk.ConformanceFixtures.MAPPER;
import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.superdurable.dexconnectors.sdk.ConformanceFixtures;
import io.superdurable.dexconnectors.sdk.ConnectionRef;
import io.superdurable.dexconnectors.sdk.pollingtrigger.TriggerCredentialCalls;
import io.superdurable.dexconnectors.sdk.trigger.TriggerCancellation;
import io.superdurable.dexconnectors.sdk.trigger.TriggerContext;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDelivery;
import io.superdurable.dexconnectors.sdk.trigger.TriggerEvent;
import io.superdurable.dexconnectors.sdk.trigger.TriggerRef;
import io.superdurable.dexconnectors.sdk.trigger.TriggerTarget;
import io.superdurable.dexconnectors.sdk.trigger.UndeliverableTriggerException;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.time.OffsetDateTime;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Base64;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class TriggerInboxTest {
    private static final Scope SCOPE = new Scope("test", "live");
    private static final TriggerInboxKey KEY = new TriggerInboxKey("slack", "support", "threadReplyCreated", "reply");
    private static final Path GO_FIXTURES = Paths.get(ConformanceFixtures.directory().getPath(), "..", "sdkpython",
            "tests", "fixtures");

    @Test
    void storesEventsUnderTheGoKeyAndDocumentOncePerIdInOrder() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final TriggerInbox inbox = new TriggerInbox(objects, SCOPE, KEY);
        assertEquals("projects/test/live/trigger-inboxes/slack/c3VwcG9ydA/threadReplyCreated/cmVwbHk/head",
                inbox.getKey());
        for (String eventId : Arrays.asList("Ev1", "Ev1", "Ev2")) {
            inbox.add(new PendingTriggerEvent(eventId, TriggerEventCodec.encodeTriggerEvent(textEvent(eventId), null)));
        }
        assertEquals(Arrays.asList("Ev1", "Ev2"), ids(inbox.pending()));
        final ProjectObject stored = objects.readObject(inbox.getKey(), "");
        assertTrue(new String(stored.getContents(), StandardCharsets.UTF_8).startsWith(
                "{\"schemaVersion\":\"connectors.dex.dev/project-trigger-inbox/v1alpha1\",\"events\":[{\"id\":\"Ev1\","
                        + "\"event\":{\"eventId\":\"Ev1\",\"occurredAt\":\"2025-10-12T00:00:00.12Z\",\"payload\":"
                        + "{\"text\":\"hello \\u003cworld\\u003e \\u0026 co\"}}}"));
        assertEquals(GoCompatibleJson.sha256Hex(stored.getContents()) + "-" + stored.getVersion(), stored.getEtag());
        inbox.remove("Ev1");
        inbox.remove("absent");
        assertEquals(Arrays.asList("Ev2"), ids(inbox.pending()));
    }

    @Test
    void keysAndEmptyHeadMatchConformance() throws Exception {
        final JsonNode storage = ConformanceFixtures.load("project-storage");
        final Scope scope = Scope.fromJson(storage.get("scope"));
        final TriggerInboxKey key = new TriggerInboxKey("sample",
                storage.get("connectionKey").get("connectionName").textValue(), "itemCreated", "main / west");
        assertEquals(storage.get("triggerInboxHeadKey").textValue(), key.path(scope));
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final TriggerInbox inbox = new TriggerInbox(objects, SCOPE, KEY);
        assertTrue(inbox.pending().isEmpty());
        assertArrayEquals(GoCompatibleJson.encode(storage.get("emptyTriggerInboxDocument")),
                objects.readObject(inbox.getKey(), "").getContents());
        final byte[] document = GoCompatibleJson.encode(storage.get("triggerInboxDocument"));
        assertEquals(Arrays.asList("event-é-1"), ids(TriggerInbox.decodeDocument(document)));
        assertArrayEquals(document, TriggerInbox.encodeDocument(TriggerInbox.decodeDocument(document)));
        final StringBuilder longName = new StringBuilder();
        for (int index = 0; index < 256; index++) {
            longName.append('a');
        }
        new TriggerInboxKey("slack", longName.toString(), "itemCreated", "b").validate();
        for (TriggerInboxKey invalid : Arrays.asList(
                new TriggerInboxKey("slack", longName + "a", "itemCreated", "b"),
                new TriggerInboxKey("slack\n", "a", "itemCreated", "b"),
                new TriggerInboxKey("slack", "a", "itemCreated\n", "b"),
                new TriggerInboxKey("slack", " padded", "itemCreated", "b"),
                new TriggerInboxKey("slack", "a", "itemCreated", "b　"),
                new TriggerInboxKey("slack", "line\nbreak", "itemCreated", "b"),
                new TriggerInboxKey("slack", "", "itemCreated", "b"))) {
            assertThrows(IllegalArgumentException.class, () -> new TriggerInbox(new InMemoryObjectStore(), SCOPE, invalid));
        }
    }

    @Test
    void keepsGoEventsByteForByteAndRewritesAsGoDoes() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final TriggerInbox inbox = inboxWith(objects, goFixture("go-trigger-inbox-three-events.json"));
        final List<PendingTriggerEvent> pending = inbox.pending();
        assertEquals(Arrays.asList("Ev1<&>", "Ev2", "Ev3"), ids(pending));
        assertTrue(new String(pending.get(1).getEvent(), StandardCharsets.UTF_8).contains(
                "\"alpha\":9007199254740993,\"list\":[1.50,\"x\"],\"nested\":{\"a\":null,\"b\":true},\"zeta\":1"));
        final byte[] four = goFixture("go-trigger-inbox-four-events.json");
        inbox.add(inboxWith(new InMemoryObjectStore(), four).pending().get(3));
        assertArrayEquals(four, objects.readObject(inbox.getKey(), "").getContents());
        inbox.remove("Ev2");
        assertArrayEquals(goFixture("go-trigger-inbox-after-remove.json"),
                objects.readObject(inbox.getKey(), "").getContents());
    }

    @Test
    void decodesAndRewritesEveryGoDecodingCase() throws Exception {
        final JsonNode cases = MAPPER.readTree(goFixture("go-trigger-inbox-decoding-cases.json"));
        assertTrue(cases.size() > 8);
        for (JsonNode decodingCase : cases) {
            final String name = decodingCase.get("name").textValue();
            final InMemoryObjectStore objects = new InMemoryObjectStore();
            final TriggerInbox inbox = inboxWith(objects, Base64.getDecoder().decode(decodingCase.get("input").textValue()));
            if (decodingCase.get("error").booleanValue()) {
                final IllegalArgumentException error = assertThrows(IllegalArgumentException.class, inbox::pending, name);
                assertEquals("trigger inbox document is invalid", error.getMessage(), name);
                continue;
            }
            final List<String> expectedIds = new ArrayList<>();
            for (JsonNode id : decodingCase.get("ids")) {
                expectedIds.add(id.textValue());
            }
            assertEquals(expectedIds, ids(inbox.pending()), name);
            inbox.add(new PendingTriggerEvent("EvAdded", "{\"added\":true}".getBytes(StandardCharsets.UTF_8)));
            assertEquals(decodingCase.get("output").textValue(),
                    Base64.getEncoder().encodeToString(objects.readObject(inbox.getKey(), "").getContents()), name);
        }
    }

    @Test
    void refusesDocumentsGoRefuses() throws Exception {
        final String schema = "\"schemaVersion\":\"connectors.dex.dev/project-trigger-inbox/v1alpha1\"";
        for (String invalid : Arrays.asList(
                "{\"schemaVersion\":\"other\",\"events\":[]}",
                "{" + schema + ",\"events\":[],\"extra\":1}",
                "{" + schema + ",\"events\":[{\"id\":\"Ev1\",\"event\":{},\"extra\":1}]}",
                "{" + schema + ",\"events\":[]} {}")) {
            final TriggerInbox inbox = inboxWith(new InMemoryObjectStore(), invalid.getBytes(StandardCharsets.UTF_8));
            assertThrows(IllegalArgumentException.class, inbox::pending, invalid);
        }
        final TriggerInbox empty = inboxWith(new InMemoryObjectStore(),
                ("{" + schema + ",\"events\":null}").getBytes(StandardCharsets.UTF_8));
        assertTrue(empty.pending().isEmpty());
    }

    @Test
    void replaysGoPayloadsWithExactIntegersAndCustomDecoders() throws Exception {
        final TriggerInbox inbox = inboxWith(new InMemoryObjectStore(), goFixture("go-trigger-inbox-three-events.json"));
        final List<TriggerEvent<JsonNode>> delivered = new ArrayList<>();
        new DurableTriggerTarget<JsonNode>(inbox, (context, event) -> delivered.add(event),
                TriggerEventCodec.jsonPayloadDecoder(), null, null).replayTriggerDeliveries(new TriggerContext());
        assertEquals(Arrays.asList("Ev1<&>", "Ev2", "Ev3"), eventIds(delivered));
        assertEquals(OffsetDateTime.parse("2025-10-12T00:00:00.123456789Z"), delivered.get(0).getOccurredAt());
        final String text = delivered.get(0).getPayload().get("text").textValue();
        assertTrue(text.startsWith("<b>approve</b> & ship"), text);
        assertTrue(text.endsWith("ünïcode \"quoted\"\n\b\f\u0001"), text);
        assertEquals("9007199254740993", delivered.get(2).getPayload().get("count").asText());
        assertTrue(inbox.pending().isEmpty());

        final TriggerInbox exactInbox = inboxWith(new InMemoryObjectStore(),
                goFixture("go-trigger-inbox-three-events.json"));
        final List<String> texts = new ArrayList<>();
        new DurableTriggerTarget<String>(exactInbox, (context, event) -> { }, (payloadJson, payloadBytes) -> {
            texts.add(payloadJson);
            return payloadJson;
        }, null, null).replayTriggerDeliveries(new TriggerContext());
        assertEquals(Arrays.asList(
                "{\"alpha\":9007199254740993,\"list\":[1.50,\"x\"],\"nested\":{\"a\":null,\"b\":true},\"zeta\":1}",
                "{\"count\":9007199254740993,\"label\":\"big\"}"), texts.subList(1, 3));
    }

    @Test
    void decoderReceivesGoTextAndAPrivateCopyOfTheBytes() throws Exception {
        final byte[] event = new byte[] {'{', '"', 'e', 'v', 'e', 'n', 't', 'I', 'd', '"', ':', '"', 'E', 'v', '1', '"',
            ',', '"', 'o', 'c', 'c', 'u', 'r', 'r', 'e', 'd', 'A', 't', '"', ':', '"', '2', '0', '2', '5', '-', '1',
            '0', '-', '1', '2', 'T', '0', '0', ':', '0', '0', ':', '0', '0', 'Z', '"', ',', '"', 'p', 'a', 'y', 'l',
            'o', 'a', 'd', '"', ':', '{', '"', 't', '"', ':', '"', 'a', (byte) 0xff, 'b', '"', '}', '}'};
        final byte[] original = event.clone();
        final List<String> received = new ArrayList<>();
        final TriggerEvent<String> decoded = TriggerEventCodec.decodeTriggerEvent(event, (payloadJson, payloadBytes) -> {
            received.add(payloadJson);
            assertArrayEquals(new byte[] {'{', '"', 't', '"', ':', '"', 'a', (byte) 0xff, 'b', '"', '}'}, payloadBytes);
            payloadBytes[0] = 'x';
            return payloadJson;
        });
        assertEquals(Arrays.asList("{\"t\":\"a�b\"}"), received);
        assertEquals("Ev1", decoded.getId());
        assertArrayEquals(original, event);
    }

    @Test
    void durableTargetRecordsConsumesAndReplaysAfterRestart() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final List<String> handled = new ArrayList<>();
        final TriggerTarget<JsonNode> failing = (context, event) -> {
            throw new IOException("target unavailable");
        };
        final DurableTriggerTarget<JsonNode> first = new DurableTriggerTarget<JsonNode>(
                new TriggerInbox(objects, SCOPE, KEY), failing, TriggerEventCodec.jsonPayloadDecoder(), null, null);
        final TriggerEvent<JsonNode> event = jsonEvent("Ev1");
        first.prepareTrigger(new TriggerContext(), event);
        assertThrows(IOException.class, () -> first.handleTrigger(new TriggerContext(), event));
        assertTrue(TriggerDelivery.hasDurableTriggerInbox(first));

        final DurableTriggerTarget<JsonNode> restarted = new DurableTriggerTarget<JsonNode>(
                new TriggerInbox(objects, SCOPE, KEY), (context, replayed) -> handled.add(replayed.getId()),
                TriggerEventCodec.jsonPayloadDecoder(), null, null);
        restarted.replayTriggerDeliveries(new TriggerContext());
        assertEquals(Arrays.asList("Ev1"), handled);
        assertTrue(new TriggerInbox(objects, SCOPE, KEY).pending().isEmpty());

        final DurableTriggerTarget<JsonNode> skipping = new DurableTriggerTarget<JsonNode>(
                new TriggerInbox(objects, SCOPE, KEY), (context, skipped) -> {
                    throw new UndeliverableTriggerException(new IllegalStateException("closed"));
                }, TriggerEventCodec.jsonPayloadDecoder(), null, null);
        skipping.prepareTrigger(new TriggerContext(), jsonEvent("Ev2"));
        skipping.replayTriggerDeliveries(new TriggerContext());
        assertTrue(new TriggerInbox(objects, SCOPE, KEY).pending().isEmpty());
    }

    @Test
    void removesWithoutReinvokingAfterAFailedRemoval() throws Exception {
        final InMemoryObjectStore backing = new InMemoryObjectStore();
        final AtomicInteger failingSwaps = new AtomicInteger(1);
        final ObjectStore flaky = new ObjectStore() {
            @Override
            public ProjectObject readObject(final String key, final String version) throws IOException {
                return backing.readObject(key, version);
            }

            @Override
            public ProjectObject createObject(final String key, final byte[] contents) throws IOException {
                return backing.createObject(key, contents);
            }

            @Override
            public ProjectObject compareAndSwapObject(final String key, final String expectedEtag, final byte[] contents)
                    throws IOException {
                if (contents.length < 120 && failingSwaps.getAndDecrement() > 0) {
                    throw new IOException("storage unavailable");
                }
                return backing.compareAndSwapObject(key, expectedEtag, contents);
            }
        };
        final AtomicInteger calls = new AtomicInteger();
        final DurableTriggerTarget<JsonNode> durable = new DurableTriggerTarget<JsonNode>(
                new TriggerInbox(flaky, SCOPE, KEY), (context, event) -> calls.incrementAndGet(),
                TriggerEventCodec.jsonPayloadDecoder(), null, null);
        final TriggerEvent<JsonNode> event = jsonEvent("Ev1");
        durable.prepareTrigger(new TriggerContext(), event);
        assertThrows(IOException.class, () -> durable.handleTrigger(new TriggerContext(), event));
        durable.handleTrigger(new TriggerContext(), event);
        assertEquals(1, calls.get());
        assertTrue(new TriggerInbox(backing, SCOPE, KEY).pending().isEmpty());
    }

    @Test
    void prepareRefusesWritesAfterCancellation() throws Exception {
        final InMemoryObjectStore objects = new InMemoryObjectStore();
        final DurableTriggerTarget<JsonNode> durable = new DurableTriggerTarget<JsonNode>(
                new TriggerInbox(objects, SCOPE, KEY), (context, event) -> { },
                TriggerEventCodec.jsonPayloadDecoder(), null, null);
        final TriggerCancellation cancellation = new TriggerCancellation();
        cancellation.cancel();
        assertThrows(RuntimeException.class,
                () -> durable.prepareTrigger(new TriggerContext(cancellation), jsonEvent("Ev1")));
        assertTrue(new TriggerInbox(objects, SCOPE, KEY).pending().isEmpty());
    }

    @Test
    void boundsTheBacklog() throws Exception {
        final TriggerInbox inbox = new TriggerInbox(new InMemoryObjectStore(), SCOPE, KEY);
        for (int index = 0; index < TriggerInbox.MAXIMUM_PENDING_TRIGGER_EVENTS; index++) {
            inbox.add(new PendingTriggerEvent("Ev" + index, "{}".getBytes(StandardCharsets.UTF_8)));
        }
        assertThrows(TriggerInboxFullException.class,
                () -> inbox.add(new PendingTriggerEvent("EvOver", "{}".getBytes(StandardCharsets.UTF_8))));
        assertThrows(IllegalArgumentException.class,
                () -> inbox.add(new PendingTriggerEvent(" ", "{}".getBytes(StandardCharsets.UTF_8))));
        assertThrows(IllegalArgumentException.class,
                () -> inbox.add(new PendingTriggerEvent("EvBad", "{".getBytes(StandardCharsets.UTF_8))));
    }

    @Test
    void derivesGoTriggerCredentialCalls() throws Exception {
        final JsonNode cases = MAPPER.readTree(goFixture("go-poll-cursor-cases.json")).get("credentialCalls");
        assertEquals(3, cases.size());
        for (JsonNode credentialCase : cases) {
            assertEquals(credentialCase.get("callId").textValue(), TriggerCredentialCalls.newTriggerCredentialCall(
                    new ConnectionRef(credentialCase.get("provider").textValue(), credentialCase.get("connection").textValue()),
                    new TriggerRef(credentialCase.get("connectorId").textValue(),
                            credentialCase.get("triggerName").textValue())).getId().value());
        }
    }

    private static TriggerInbox inboxWith(final InMemoryObjectStore objects, final byte[] contents) throws IOException {
        final TriggerInbox inbox = new TriggerInbox(objects, SCOPE, KEY);
        objects.createObject(inbox.getKey(), contents);
        return inbox;
    }

    private static byte[] goFixture(final String name) throws IOException {
        return Files.readAllBytes(GO_FIXTURES.resolve(name));
    }

    private static TriggerEvent<ObjectNode> textEvent(final String eventId) {
        final ObjectNode payload = MAPPER.createObjectNode().put("text", "hello <world> & co");
        return new TriggerEvent<ObjectNode>(eventId, OffsetDateTime.parse("2025-10-12T00:00:00.120Z"), payload);
    }

    private static TriggerEvent<JsonNode> jsonEvent(final String eventId) {
        return new TriggerEvent<JsonNode>(eventId, OffsetDateTime.parse("2025-10-12T00:00:00Z"),
                MAPPER.createObjectNode().put("text", "hello"));
    }

    private static List<String> ids(final List<PendingTriggerEvent> events) {
        final List<String> ids = new ArrayList<>();
        for (PendingTriggerEvent event : events) {
            ids.add(event.getId());
        }
        return ids;
    }

    private static <P> List<String> eventIds(final List<TriggerEvent<P>> events) {
        final List<String> ids = new ArrayList<>();
        for (TriggerEvent<P> event : events) {
            ids.add(event.getId());
        }
        return ids;
    }
}
