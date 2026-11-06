// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, expect, it } from "vitest";
import {
  Client as DexClient,
  Registry,
  Worker,
  openBlobCache,
  type BlobCache,
} from "@superdurable/dex";
import {
  SecretString,
  StaticCredentialProvider,
} from "@superdurable/connectors-sdk";
import { Client, newConnection } from "../../src/index.js";
import { ConnectionName, ReadEventFlow } from "./flow.js";
const connectionReference = { provider: "microsoft", name: ConnectionName };
const credential = {
  authMethodId: "microsoft-oauth",
  clientId: "fixture-client",
  clientSecret: new SecretString("fixture-secret"),
  accessToken: new SecretString("fixture-token"),
  refreshToken: new SecretString("fixture-refresh"),
};
const provider = new Client(
  {},
  new StaticCredentialProvider([[connectionReference, credential]]),
  {
    refreshDriver: {
      refreshRequired: () => false,
      refresh: async () => {
        throw new Error("unexpected refresh");
      },
    },
    baseURL: "http://127.0.0.1:40723/v1.0",
    transport: async (request) => {
      const id = request.url.includes("/missing") ? "" : "event-1";
      return id
        ? {
            status: 200,
            headers: {},
            body: Buffer.from(
              JSON.stringify({
                id,
                subject: "Meeting",
                start: { dateTime: "2026-02-24T17:00:00", timeZone: "UTC" },
                end: { dateTime: "2026-02-24T18:00:00", timeZone: "UTC" },
              }),
            ),
          }
        : { status: 404, headers: {}, body: new Uint8Array() };
    },
  },
);
const flow = new ReadEventFlow(newConnection(provider, connectionReference));
const registry = new Registry([flow]);
let directory: string;
let cache: BlobCache;
let dexClient: DexClient;
let worker: Worker;
beforeAll(async () => {
  directory = await mkdtemp(join(tmpdir(), "outlook-dex-test-"));
  cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  worker = new Worker(registry, cache, { serverAddress: "127.0.0.1:40722" });
  dexClient = new DexClient(registry, cache, {
    serverAddress: "127.0.0.1:40722",
  });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 15_000 });
}, 30_000);
afterAll(async () => {
  if (dexClient) await dexClient.close();
  if (worker) await worker.close();
  if (cache) cache.close();
  if (directory) await rm(directory, { recursive: true, force: true });
});
it("reads a provider event through Dex", async () => {
  const id = `outlook-read-${randomUUID()}`;
  await dexClient.startFlow(flow, id, {
    eventId: "event-1",
    timeZone: "America/Los_Angeles",
  });
  const result = await dexClient.waitForFlow(id, 20_000);
  expect(result.status).toBe("completed");
  expect(result.singleOutput()).toMatchObject({
    branch: "found",
    value: { id: "event-1", subject: "Meeting" },
  });
}, 30_000);
it("fails when the optional notFound branch is unwired", async () => {
  const id = `outlook-missing-${randomUUID()}`;
  await dexClient.startFlow(flow, id, { eventId: "missing" });
  const result = await dexClient.waitForFlow(id, 20_000);
  expect(result.status).toBe("failed");
}, 30_000);
