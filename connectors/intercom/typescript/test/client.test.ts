// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createServer } from "node:http";
import { once } from "node:events";
import { afterEach, expect, it } from "vitest";
import { SecretString, type Call } from "@superdurable/connectors-sdk";
import { BodyTooLargeError } from "@superdurable/connectors-sdk/providerhttp";
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
