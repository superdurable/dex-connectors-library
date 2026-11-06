// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { Registry, Worker, openBlobCache } from "@superdurable/dex";
import { loadFromEnvironment } from "@superdurable/connectors-sdk/projectconfig";
import { newProjectOutlookCalendarConnection } from "../../src/index.js";
import { ConnectionName, ReadEventFlow } from "./flow.js";
/** Start the checked-in Outlook Calendar Flow with project credentials. */
export async function runReadEventWorker(): Promise<void> {
  const project = await loadFromEnvironment();
  const connection = newProjectOutlookCalendarConnection(
    project,
    ConnectionName,
  );
  const registry = new Registry([new ReadEventFlow(connection)]);
  const directory = await mkdtemp(join(tmpdir(), "outlook-calendar-example-"));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  try {
    const worker = new Worker(registry, cache, {
      serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? "127.0.0.1:40722",
    });
    const stop = () => {
      void worker.close();
    };
    process.once("SIGINT", stop);
    process.once("SIGTERM", stop);
    try {
      await worker.run();
    } finally {
      process.off("SIGINT", stop);
      process.off("SIGTERM", stop);
      await worker.close();
    }
  } finally {
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  runReadEventWorker().catch(() => {
    process.stderr.write("Outlook Calendar example Worker failed\n");
    process.exitCode = 1;
  });
