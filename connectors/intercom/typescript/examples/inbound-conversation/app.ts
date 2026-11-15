// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from "node:events";
import { createServer } from "node:http";
import { Client, Registry, Worker, openBlobCache } from "@superdurable/dex";
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, withTriggerLoggerAttributes, type TriggerLogger } from "@superdurable/connectors-sdk";
import type { LoadedProject } from "@superdurable/connectors-sdk/projectconfig";
import { ConnectorID, ConversationEventTriggerDefinition, newProjectConnection, newProjectConversationEventEndpointRunner, type IntercomClientOptions } from "../../src/index.js";
import { ConnectionName, InboundConversationFlow, InboundTriggerBinding, acceptInboundConversation, mapToFlowInput, resolveFlowId } from "./flow.js";

/** Path to enter, behind an HTTPS tunnel, as the Intercom app's webhook URL. */
export const WebhookPath = "/webhooks/intercom";
/** Readiness path that answers 200 once the binding receives notifications. */
export const ReadinessPath = "/readyz";

/** Settings of one inbound-conversation application process. */
export interface InboundConversationApplicationOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Host and port of the webhook HTTP server; port 0 picks a free port. */ webhookHost: string; webhookPort: number;
  /** Blob cache directory owned by the caller. */ blobCacheDirectory: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Intercom client options, such as a local API stand-in for tests. */ intercomOptions?: IntercomClientOptions;
  /** Called once the webhook listens and the Worker is registered. */ onReady?(ready: { client: Client; flow: InboundConversationFlow; webhookURL: string }): void;
}

/**
 * Serve the Intercom webhook at once, so notifications are recorded in the binding's durable inbox even
 * while Dex is unreachable, and run the Worker until signal aborts or the endpoint runner fails.
 */
export async function runInboundConversationApplication(project: LoadedProject, options: InboundConversationApplicationOptions, signal: AbortSignal): Promise<void> {
  const intercomOptions: IntercomClientOptions = { ...options.intercomOptions, logger: options.logger };
  const flow = new InboundConversationFlow(newProjectConnection(project, ConnectionName, intercomOptions));
  const registry = new Registry([flow]);
  const cache = openBlobCache({ directory: options.blobCacheDirectory, maxBytes: 1 << 30 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = withTriggerLoggerAttributes(options.logger, {
    connector: ConnectorID, connection: ConnectionName, trigger: ConversationEventTriggerDefinition.trigger.triggerName, binding: InboundTriggerBinding,
  });
  // The notification ID is the Flow-start request ID, so a redelivery starts no second Flow.
  const runner = newProjectConversationEventEndpointRunner(project, ConnectionName, [{
    bindingName: InboundTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptInboundConversation, resolveFlowId, mapToFlowInput, { logger: bindingLogger }),
  }], intercomOptions);
  const server = createServer((request, response) => {
    const path = request.url?.split("?")[0];
    if (path === WebhookPath) { void runner.serveHTTP(request, response); return; }
    request.resume();
    if (path === ReadinessPath && runner.runningSourceCount() > 0) { response.writeHead(200).end("ready\n"); return; }
    response.writeHead(path === ReadinessPath ? 503 : 404).end();
  });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  if (signal.aborted) stop.abort(); else signal.addEventListener("abort", onAbort, { once: true });
  const running = runner.run(newTriggerContext(stop.signal));
  // The runner settles only when it stops; a failure stops the Worker wait below.
  const runnerResult = running.then(() => undefined, (error: unknown) => { stop.abort(); return error; });
  try {
    server.listen(options.webhookPort, options.webhookHost);
    await once(server, "listening");
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("webhook server address is unavailable");
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    options.onReady?.({ client, flow, webhookURL: `http://${options.webhookHost}:${address.port}${WebhookPath}` });
    const failure = await runnerResult;
    if (failure !== undefined && !(failure instanceof TriggerDeliveryAbortedError && signal.aborted)) throw failure;
  } finally {
    stop.abort();
    signal.removeEventListener("abort", onAbort);
    await runnerResult;
    await new Promise<void>(resolve => server.close(() => resolve()));
    await client.close(); await worker.close(); cache.close();
  }
}
