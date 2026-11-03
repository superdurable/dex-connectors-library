// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createServer } from "node:http";
import { once } from "node:events";
import { afterEach, expect, it } from "vitest";
import { SecretString, type Call } from "@superdurable/connectors-sdk";
import { AddressRefusal, BodyTooLargeError, ProviderTransportError } from "@superdurable/connectors-sdk/providerhttp";
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

it("keeps a possibly-sent reply checkpoint until read-back confirms the part", async () => {
  let heartbeat: unknown;
  const paths: string[] = [];
  const replyCall = {
    ...call,
    context: {
      ...call.context,
      hasLastHeartbeatValue: () => heartbeat !== undefined && heartbeat !== null,
      getLastHeartbeatValue: () => heartbeat,
      recordHeartbeat: async (value: unknown) => { heartbeat = value; },
    },
  } as Call;
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
  expect(heartbeat).toBeTruthy();
  const second = await client.replyToConversation().invoke(replyCall, input);
  expect(second.kind).toBe("branch");
  if (second.kind === "branch") {
    expect(second.branch).toBe("replied");
    expect(second.value.wasAlreadyApplied).toBe(true);
  }
  expect(paths).toEqual(["/conversations/2002/reply", "/conversations/2002"]);
});

it("treats address refusal as unsent and clears the reply checkpoint", async () => {
  let heartbeat: unknown;
  const replyCall = {
    ...call,
    context: {
      ...call.context,
      hasLastHeartbeatValue: () => heartbeat !== undefined && heartbeat !== null,
      getLastHeartbeatValue: () => heartbeat,
      recordHeartbeat: async (value: unknown) => { heartbeat = value; },
    },
  } as Call;
  const client = new IntercomClient({}, validCredentials, {
    endpoint: "http://127.0.0.1:0",
    send: async () => { throw new AddressRefusal("blocked address"); },
  });
  const attempt = await client.replyToConversation().invoke(replyCall, {
    conversationId: "2002", adminId: "77", messageType: "comment", body: "Hello",
  });
  expect(attempt.kind).toBe("retry");
  expect(heartbeat).toBeNull();
});

it("uses Go whitespace for reply body validation and HTML", async () => {
  let sentBody = "";
  const replyCall = {
    ...call,
    context: { ...call.context, hasLastHeartbeatValue: () => false, recordHeartbeat: async () => {} },
  } as Call;
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
