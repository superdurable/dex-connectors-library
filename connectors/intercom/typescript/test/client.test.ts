// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createServer } from "node:http";
import { once } from "node:events";
import { afterEach, expect, it } from "vitest";
import { ReauthorizationRequiredError, SecretString, type Call } from "@superdurable/connectors-sdk";
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from "@superdurable/connectors-sdk/projectconfig";
import { AddressRefusal, BodyTooLargeError, ProviderStalledError, ProviderTransportError } from "@superdurable/connectors-sdk/providerhttp";
import { IntercomClient } from "../src/client.js";

const call = {
  id: "bd281437-1e89-5a3e-b61d-5323c0aa792e",
  context: { cancellationSignal: new AbortController().signal },
} as Call;
const validCredentials = {
  resolve: async () => ({ accessToken: new SecretString("test-token") }),
};
const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => {
  await Promise.all(
    servers
      .splice(0)
      .map(
        (server) =>
          new Promise<void>((resolve) => server.close(() => resolve())),
      ),
  );
});

it("rejects missing credentials and invalid region before a provider request", async () => {
  expect(
    () => new IntercomClient({ region: "unknown" }, validCredentials),
  ).toThrow(/region/);
  const client = new IntercomClient(
    {},
    {
      resolve: async () => {
        throw new Error("secret unavailable");
      },
    },
    { endpoint: "http://127.0.0.1:0" },
  );
  const attempt = await client
    .getConversation()
    .invoke(call, { conversationId: "2002" });
  expect(attempt.kind).toBe("branch");
  if (attempt.kind === "branch") {
    expect(attempt.branch).toBe("defect");
    expect(attempt.failure?.kind).toBe("AUTHENTICATION");
  }
});

it("sends the version and bearer token to a provider stand-in on a free port", async () => {
  const observed: { path?: string; authorization?: string; version?: string } =
    {};
  const server = createServer((request, response) => {
    observed.path = request.url;
    observed.authorization = request.headers.authorization;
    observed.version = String(request.headers["intercom-version"] ?? "");
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        type: "conversation",
        id: "2002",
        state: "open",
        open: true,
        source: {},
        created_at: 1793008800,
        updated_at: 1793008800,
      }),
    );
  });
  servers.push(server);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (typeof address === "string" || address === null)
    throw new Error("missing provider stand-in address");
  const client = new IntercomClient({}, validCredentials, {
    endpoint: `http://127.0.0.1:${address.port}`,
  });
  const attempt = await client
    .getConversation()
    .invoke(call, { conversationId: "2002" });
  expect(attempt.kind).toBe("branch");
  if (attempt.kind === "branch") {
    expect(attempt.branch).toBe("found");
    expect(attempt.value.conversation.id).toBe("2002");
  }
  expect(observed).toEqual({
    path: "/conversations/2002?display_as=plaintext",
    authorization: "Bearer test-token",
    version: "2.16",
  });
});

it("accepts RFC 5322 bare addresses but rejects a display name", async () => {
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => ({
      status: 200,
      headers: {},
      body: Buffer.from('{"type":"list","data":[],"total_count":0}'),
    }),
  });
  const address = await client
    .findContactByEmail()
    .invoke(call, { email: "é@example.com" });
  expect(address.kind).toBe("branch");
  if (address.kind === "branch") expect(address.branch).toBe("notFound");
  const displayName = await client
    .findContactByEmail()
    .invoke(call, { email: "Ada <ada@example.com>" });
  expect(displayName.kind).toBe("branch");
  if (displayName.kind === "branch") expect(displayName.branch).toBe("defect");
});

it("truncates conversation text at a UTF-8 boundary and flags it", async () => {
  const body = `${"a".repeat(16383)}é-end`;
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => ({
      status: 200,
      headers: {},
      body: Buffer.from(
        JSON.stringify({
          type: "conversation",
          id: "2002",
          state: "open",
          open: true,
          source: { body },
          created_at: 1793008800,
          updated_at: 1793008800,
        }),
      ),
    }),
  });
  const attempt = await client
    .getConversation()
    .invoke(call, { conversationId: "2002" });
  expect(attempt.kind).toBe("branch");
  if (attempt.kind === "branch") {
    expect(attempt.branch).toBe("found");
    expect(attempt.value.conversation.source.body).toBe("a".repeat(16383));
    expect(attempt.value.conversation.source.isBodyTruncated).toBe(true);
  }
});

it.each([429, 503])("classifies oversized HTTP %i by status", async (status) => {
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => {
      throw new BodyTooLargeError(status, {});
    },
  });
  const attempt = await client.getConversation().invoke(call, { conversationId: "2002" });
  expect(attempt.kind).toBe("retry");
});

it("retries a local dial refusal before any request reaches Intercom", async () => {
  const server = createServer();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (typeof address === "string" || address === null) throw new Error("missing local address");
  await new Promise<void>((resolve) => server.close(() => resolve()));
  const client = new IntercomClient({}, validCredentials, {
    endpoint: `http://127.0.0.1:${address.port}`,
  });
  const attempt = await client.getConversation().invoke(call, { conversationId: "2002" });
  expect(attempt.kind).toBe("retry");
});

it.each([
  { open: "true" },
  { created_at: true },
  { state: 4 },
  { source: { body: 8 } },
])("rejects wrong typed provider fields", async (invalidField) => {
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => ({
      status: 200,
      headers: {},
      body: Buffer.from(JSON.stringify({ id: "2002", ...invalidField })),
    }),
  });
  const attempt = await client.getConversation().invoke(call, { conversationId: "2002" });
  expect(attempt.kind).toBe("branch");
  if (attempt.kind === "branch") expect(attempt.branch).toBe("invalidResponse");
});

it.each([
  ["2024-02-29T12:00:00Z", "searched"],
  ["2025-02-29T12:00:00Z", "defect"],
  ["2025-02-30T12:00:00Z", "defect"],
  ["2025-03-01T24:00:00Z", "defect"],
  ["2025-03-01T12:00:00+99:59", "defect"],
  ["2025-03-01T12:00:00,5Z", "searched"],
  ["2025-03-01T9:00:00Z", "searched"],
  ["2025-03-01T12:00:00+24:00", "searched"],
  ["2025-03-01T12:00:00+00:60", "searched"],
] as const)("validates Go RFC 3339 instant %s", async (updatedSince, expectedBranch) => {
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => ({
      status: 200,
      headers: {},
      body: Buffer.from('{"type":"conversation.list","conversations":[],"total_count":0}'),
    }),
  });
  const attempt = await client.searchConversations().invoke(call, { updatedSince });
  expect(attempt.kind).toBe("branch");
  if (attempt.kind === "branch") expect(attempt.branch).toBe(expectedBranch);
});

it.each([
  ["-@[]", "defect"],
  ["a\u0085b@example.com", "notFound"],
  ["a\ufeffb@example.com", "notFound"],
  ["a@[127.0.0.1]", "notFound"],
  ["a@[IPv6:::1]", "defect"],
] as const)("matches Go mail.ParseAddress for %s", async (email, expectedBranch) => {
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => ({ status: 200, headers: {}, body: Buffer.from('{"type":"list","data":[],"total_count":0}') }),
  });
  const attempt = await client.findContactByEmail().invoke(call, { email });
  expect(attempt.kind).toBe("branch");
  if (attempt.kind === "branch") expect(attempt.branch).toBe(expectedBranch);
});

it.each([
  ["1", 1_000], [" 1 ", 1_000], ["01", 1_000], ["1.5", 0],
  ["-1", 0], ["0", 0], ["3600", 60_000],
  ["999999999999999999999999", 60_000],
] as const)("matches Go Retry-After duration for %s", async (header, expectedMs) => {
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => ({ status: 429, headers: { "retry-after": header }, body: Buffer.from("{}") }),
  });
  const exchange = await client.exchange(call, "getConversation", "GET", "/conversations/2002");
  expect(exchange.outcome).toBe("rateLimited");
  if (exchange.outcome === "rateLimited") expect(exchange.retryAfterMs).toBe(expectedMs);
});

/** A Dex attempt context whose heartbeat persists across attempts of one Step execution. */
function fencedReplyCall() {
  const state: { heartbeat: unknown; writes: unknown[]; attempt: number } = { heartbeat: undefined, writes: [], attempt: 1 };
  const replyCall = {
    ...call,
    context: {
      ...call.context,
      firstAttemptAt: new Date(1_000),
      get attempt() { return state.attempt; },
      hasLastHeartbeatValue: () => state.heartbeat !== undefined && state.heartbeat !== null,
      getLastHeartbeatValue: () => state.heartbeat,
      recordHeartbeat: async (value: unknown) => { state.heartbeat = value; state.writes.push(value); },
    },
  } as unknown as Call;
  return { replyCall, state };
}

it("reads a possibly-sent reply back before any resend", async () => {
  const paths: string[] = [];
  const { replyCall, state } = fencedReplyCall();
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    now: () => new Date("2026-10-30T00:00:00Z"),
    send: async (request) => {
      paths.push(new URL(request.url).pathname);
      if (paths.length === 1) throw new ProviderTransportError(false);
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({
        type: "conversation", id: "2002", state: "open", open: true,
        conversation_parts: { conversation_parts: [{ id: "3001", part_type: "comment",
          body: "Hello", author: { type: "admin", id: "77" }, created_at: 1793318400 }], total_count: 1 },
      })) };
    },
  });
  const input = { conversationId: "2002", adminId: "77", messageType: "comment" as const, body: "Hello" };
  const first = await client.replyToConversation().invoke(replyCall, input);
  expect(first.kind).toBe("retry");
  expect(state.heartbeat).toMatchObject({ callId: call.id, attempt: 1, state: "claimed" });
  state.attempt = 2;
  const second = await client.replyToConversation().invoke(replyCall, input);
  expect(second.kind).toBe("branch");
  if (second.kind === "branch") {
    expect(second.branch).toBe("replied");
    expect(second.value.wasAlreadyApplied).toBe(true);
  }
  expect(paths).toEqual(["/conversations/2002/reply", "/conversations/2002"]);
});

it("treats address refusal as unsent and records that the reply can be resent", async () => {
  const { replyCall, state } = fencedReplyCall();
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => { throw new AddressRefusal("blocked address"); },
  });
  const attempt = await client.replyToConversation().invoke(replyCall, {
    conversationId: "2002", adminId: "77", messageType: "comment", body: "Hello",
  });
  expect(attempt.kind).toBe("retry");
  expect(state.writes).toHaveLength(2);
  expect(state.writes[0]).toMatchObject({ callId: call.id, state: "claimed" });
  expect(state.heartbeat).toMatchObject({ callId: call.id, state: "canResend" });
});

const replyInput = { conversationId: "2002", adminId: "77", messageType: "comment" as const, body: "Hello" };

/** Conversation 2002 with the reply part when isFound, or with no parts. */
function conversationBody(isFound: boolean): Uint8Array {
  const parts = isFound
    ? [{ id: "3001", part_type: "comment", body: "Hello", author: { type: "admin", id: "77" }, created_at: 1793318400 }]
    : [];
  return Buffer.from(JSON.stringify({
    type: "conversation", id: "2002", state: "open", open: true,
    conversation_parts: { conversation_parts: parts, total_count: parts.length },
  }));
}

const unusableReplyAnswers = [
  ["body breaks after the headers", () => { throw new ProviderTransportError(false, 200, {}); }],
  ["body stalls after the headers", () => { throw new ProviderStalledError(200, {}); }],
  ["body exceeds the limit", () => { throw new BodyTooLargeError(200, {}); }],
  ["body reflects the credential", () => ({ status: 200, headers: {}, body: Buffer.from('{"token":"test-token"}') })],
] as const;

it.each(unusableReplyAnswers)("reads back a 2xx whose %s and never resends it", async (_name, answer) => {
  for (const [readBack, expectedBranch] of [["found", "replied"], ["absent", "uncertain"]] as const) {
    const methods: string[] = [];
    const { replyCall } = fencedReplyCall();
    const client = new IntercomClient({}, validCredentials, {
      endpoint: "http://127.0.0.1:0",
      now: () => new Date("2026-10-30T00:00:00Z"),
      send: async (request) => {
        methods.push(request.method);
        if (request.method === "POST") return answer();
        return { status: 200, headers: {}, body: conversationBody(readBack === "found") };
      },
    });
    const attempt = await client.replyToConversation().invoke(replyCall, replyInput);
    expect(attempt.kind, "a 2xx was accepted, so the attempt never retries").not.toBe("retry");
    if (attempt.kind === "branch") expect(attempt.branch).toBe(expectedBranch);
    else expect(attempt.kind).toBe(expectedBranch);
    if (attempt.kind === "branch") expect(attempt.value.wasAlreadyApplied ?? false).toBe(false);
    expect(methods).toEqual(["POST", "GET"]);
  }
});

it.each(unusableReplyAnswers)("keeps reading back a 2xx whose %s when the read-back is rate limited", async (_name, answer) => {
  for (const [laterReadBack, expectedBranch] of [["found", "replied"], ["absent", "uncertain"]] as const) {
    const methods: string[] = [];
    const { replyCall, state } = fencedReplyCall();
    const client = new IntercomClient({}, validCredentials, {
      endpoint: "http://127.0.0.1:0",
      now: () => new Date("2026-10-30T00:00:00Z"),
      send: async (request) => {
        methods.push(request.method);
        if (request.method === "POST") return answer();
        if (methods.length === 2) return { status: 429, headers: {}, body: Buffer.from("{}") };
        return { status: 200, headers: {}, body: conversationBody(laterReadBack === "found") };
      },
    });
    expect((await client.replyToConversation().invoke(replyCall, replyInput)).kind).toBe("retry");
    state.attempt = 2;
    const attempt = await client.replyToConversation().invoke(replyCall, replyInput);
    if (attempt.kind === "branch") expect(attempt.branch).toBe(expectedBranch);
    else expect(attempt.kind).toBe(expectedBranch);
    expect(methods, "the accepted reply is only read back").toEqual(["POST", "GET", "GET"]);
  }
});

it("reads back before sending again after a resend was rate limited", async () => {
  const methods: string[] = [];
  const { replyCall, state } = fencedReplyCall();
  const answers = [
    { status: 502, headers: {}, body: Buffer.from("{}") },
    { status: 200, headers: {}, body: conversationBody(false) },
    { status: 429, headers: {}, body: Buffer.from("{}") },
    { status: 200, headers: {}, body: conversationBody(true) },
  ];
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    now: () => new Date("2026-10-30T00:00:00Z"),
    send: async (request) => {
      methods.push(request.method);
      const answer = answers.shift();
      if (!answer) throw new Error("the reply found in the read-back is never resent");
      return answer;
    },
  });
  for (const attempt of [1, 2]) {
    state.attempt = attempt;
    expect((await client.replyToConversation().invoke(replyCall, replyInput)).kind).toBe("retry");
  }
  state.attempt = 3;
  const third = await client.replyToConversation().invoke(replyCall, replyInput);
  expect(third.kind).toBe("branch");
  if (third.kind === "branch") {
    expect(third.branch).toBe("replied");
    expect(third.value.wasAlreadyApplied).toBe(true);
  }
  expect(methods).toEqual(["POST", "GET", "POST", "GET"]);
});

it.each([
  ["transient before any send", false, false, "retry"],
  ["terminal before any send", false, true, "defect"],
  ["transient after a possible send", true, false, "retry"],
  ["terminal after a possible send", true, true, "uncertain"],
] as const)("maps a %s credential failure", async (_name, hasPossibleEarlierSend, isTerminal, expected) => {
  const { replyCall, state } = fencedReplyCall();
  let requestCount = 0;
  const send = async () => {
    requestCount++;
    return { status: 502, headers: {}, body: Buffer.from("{}") };
  };
  if (hasPossibleEarlierSend) {
    const sender = new IntercomClient({}, validCredentials, { endpoint: "http://127.0.0.1:0", send });
    expect((await sender.replyToConversation().invoke(replyCall, replyInput)).kind).toBe("retry");
    state.attempt = 2;
  }
  const client = new IntercomClient({}, {
    resolve: async () => {
      if (isTerminal) return { accessToken: new SecretString("bad token with spaces") };
      throw new Error("credential store unavailable");
    },
  }, { endpoint: "http://127.0.0.1:0", send });
  const requestsBefore = requestCount;
  const attempt = await client.replyToConversation().invoke(replyCall, replyInput);
  expect(requestCount).toBe(requestsBefore);
  expect(state.heartbeat).toMatchObject({ state: hasPossibleEarlierSend ? "claimed" : "notSent" });
  if (attempt.kind === "branch") expect(attempt.branch).toBe(expected);
  else expect(attempt.kind).toBe(expected);
});

it.each([
  ["reauthorization", () => new ReauthorizationRequiredError(), "defect"],
  ["missing connection object", () => new ObjectNotFoundError(), "defect"],
  ["unselected connection", () => new ConnectionNotSelectedError(), "defect"],
  ["broker refusal", () => new CredentialBrokerRejectedError(), "defect"],
  ["broker outage", () => new CredentialBrokerUnavailableError(), "retry"],
  ["unconfigured static connection", () => new Error("connection is not configured"), "retry"],
] as const)("classifies a %s resolve failure on a first send", async (_name, error, expected) => {
  const { replyCall, state } = fencedReplyCall();
  const client = new IntercomClient({}, { resolve: async () => { throw error(); } }, {
    endpoint: "http://127.0.0.1:0",
    send: async () => { throw new Error("no request may be sent"); },
  });
  const attempt = await client.replyToConversation().invoke(replyCall, replyInput);
  expect(state.heartbeat).toMatchObject({ state: "notSent" });
  if (attempt.kind === "branch") expect(attempt.branch).toBe(expected);
  else expect(attempt.kind).toBe(expected);
});

it("treats an oversized refusal of a reply as an unknown outcome", async () => {
  const { replyCall, state } = fencedReplyCall();
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => { throw new BodyTooLargeError(400, {}); },
  });
  const attempt = await client.replyToConversation().invoke(replyCall, replyInput);
  expect(attempt.kind).toBe("retry");
  if (attempt.kind === "retry") expect(attempt.failure.kind).toBe("RESPONSE_TOO_LARGE");
  expect(state.heartbeat).toMatchObject({ state: "claimed" });
});

it("uses Go whitespace for reply body validation and HTML", async () => {
  let sentBody = "";
  const { replyCall } = fencedReplyCall();
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async (request) => {
      sentBody = Buffer.from(request.body ?? []).toString("utf8");
      return { status: 401, headers: {}, body: Buffer.from("{}") };
    },
  });
  const input = { conversationId: "2002", adminId: "77", messageType: "comment" as const };
  const blank = await client.replyToConversation().invoke(replyCall, { ...input, body: "\u0085" });
  expect(blank.kind).toBe("branch");
  if (blank.kind === "branch") expect(blank.branch).toBe("defect");
  const nonblank = await client.replyToConversation().invoke(replyCall, { ...input, body: "\ufeff" });
  expect(nonblank.kind).toBe("branch");
  if (nonblank.kind === "branch") expect(nonblank.branch).toBe("providerRejected");
  expect(JSON.parse(sentBody).body).toBe("<p>\ufeff</p>");
});
