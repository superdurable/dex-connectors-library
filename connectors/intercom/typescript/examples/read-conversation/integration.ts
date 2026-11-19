// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Client, Registry, Worker, openBlobCache } from "@superdurable/dex";
import {
  GeneratedMock,
  getConversationDefault,
  getConversationRetry,
  getConversationProviderRejectedFailure,
} from "../../src/mock.js";
import type { ConversationDetails } from "../../src/index.js";
import { ConnectionName, ReadConversationFlow } from "./flow.js";

/** Check success, retry, and optional-branch failure through a real Dex Worker and Client. */
export async function verifyReadConversationFlow(
  serverAddress: string,
): Promise<void> {
  if (!serverAddress)
    throw new TypeError(
      "DEX_FLOW_SERVICE_ADDRESS is required for the integration check",
    );
  const mock = new GeneratedMock(ConnectionName);
  const flow = new ReadConversationFlow(mock.connection());
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), "dex-intercom-integration-"));
  const blobCache = openBlobCache({ directory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, blobCache, { serverAddress });
  const client = new Client(registry, blobCache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 10_000 });
    const successID = `intercom-success-${randomUUID()}`;
    await client.startFlow(flow, successID, {
      conversationId: "2002",
      latestPartLimit: 3,
    });
    const success = await client.waitForFlow(successID, 30_000);
    assert.equal(success.status, "completed", success.errorMessage);
    assert.equal(
      success.singleOutput<ConversationDetails>().conversation.id,
      "2002",
    );
    assert.equal(mock.getConversation().calls()[0]?.input.latestPartLimit, 3);

    const retryID = `intercom-retry-${randomUUID()}`;
    mock
      .getConversation()
      .forFlow(retryID)
      .respond(
        getConversationRetry(
          {
            kind: "RATE_LIMIT",
            provider: "intercom",
            operation: "getConversation",
            message: "Intercom rate limited the read",
          },
          10,
        ),
        getConversationDefault(),
      );
    await client.startFlow(flow, retryID, { conversationId: "2002" });
    const retried = await client.waitForFlow(retryID, 30_000);
    assert.equal(retried.status, "completed", retried.errorMessage);
    assert.deepEqual(
      mock
        .getConversation()
        .calls()
        .filter((call) => call.flowId === retryID)
        .map((call) => call.retry),
      [true, undefined],
    );

    const rejectedID = `intercom-rejected-${randomUUID()}`;
    mock
      .getConversation()
      .forFlow(rejectedID)
      .respond(getConversationProviderRejectedFailure());
    await client.startFlow(flow, rejectedID, { conversationId: "2002" });
    const rejected = await client.waitForFlow(rejectedID, 30_000);
    assert.equal(rejected.status, "failed");
    assert.match(rejected.errorMessage ?? "", /providerRejected/);
    assert.match(rejected.errorMessage ?? "", /AUTHORIZATION/);
    mock.assertNoFailures();
  } finally {
    await client.close();
    await worker.close();
    blobCache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith("/integration.ts")) {
  verifyReadConversationFlow(process.env.DEX_FLOW_SERVICE_ADDRESS || "").catch(
    (error) => {
      console.error(error);
      process.exitCode = 1;
    },
  );
}
