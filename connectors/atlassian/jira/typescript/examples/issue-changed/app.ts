// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerLogger } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, newProjectIssueTriggerRunner, type JiraClientOptions } from '@superdurable/connector-atlassian-jira';
import { ConnectionName, IssueChangeFlow, IssueChangedTriggerBinding, acceptEveryIssueChange, mapToFlowInput, resolveFlowId } from './flow/workflow.js';

/** Settings of one issue-changed application process. */
export interface IssueChangedAppOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Jira client options for tests, such as a stand-in API sender. */ jiraOptions?: JiraClientOptions;
  /** Called once the Worker is registered and polling starts. */ onReady?(ready: { client: Client; flow: IssueChangeFlow; wakePoller(): void }): void;
}

/** Run the Worker and poll the saved issueChanged binding until signal aborts or the runner fails. */
export async function runIssueChangedApp(project: LoadedProject, options: IssueChangedAppOptions, signal: AbortSignal): Promise<void> {
  const jiraOptions = { ...options.jiraOptions, triggerLogger: options.logger };
  const flow = new IssueChangeFlow(newProjectConnection(project, ConnectionName, jiraOptions));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-jira-issue-changed-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  // Each change is recorded in the binding's project Trigger inbox before the poll cursor moves past it.
  const runner = newProjectIssueTriggerRunner(project, ConnectionName, [{
    bindingName: IssueChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryIssueChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
  }], jiraOptions);
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal });
    const running = runner.run(newTriggerContext(signal));
    options.onReady?.({ client, flow, wakePoller: () => runner.wakePoller(IssueChangedTriggerBinding) });
    try { await running; }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
