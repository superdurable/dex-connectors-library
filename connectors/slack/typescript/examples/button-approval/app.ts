// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newDexRPCTriggerTarget, newTriggerContext, withTriggerLoggerAttributes, type TriggerLogger } from '@superdurable/connectors-sdk';
import { decodeTriggerConfiguration, type LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import {
  ButtonClickedTriggerConfigurationSchema, ButtonClickedTriggerDefinition, ChannelThreadCreatedTriggerConfigurationSchema, ChannelThreadCreatedTriggerDefinition, ConnectorID,
  newProjectConnection, newProjectMessageTriggerRunner, type SlackClientOptions,
} from '@superdurable/connector-slack';
import {
  ButtonApprovalFlow, ButtonTriggerBinding, ConnectionName, StartTriggerBinding, mapToFlowInput, mapToReceiveButtonDecisionInput, newButtonTriggerFilter, newStartTriggerFilter,
  resolveButtonFlowId, resolveFlowId,
} from './flow/workflow.js';

/** Settings of one button-approval application process. */
export interface ButtonApprovalApplicationOptions {
  /** Dex gRPC address. */ serverAddress: string;
  /** Destination of every Trigger record. */ logger: TriggerLogger;
  /** Slack client options for tests, such as a fake Socket Mode dialer. */ slackOptions?: SlackClientOptions;
  /** Called once the Worker is registered, before the runner connects. */ onReady?(ready: { client: Client; flow: ButtonApprovalFlow; worker: Worker }): void;
}

/**
 * Run the Worker and the shared Slack Trigger runner until signal aborts or the runner fails. Clicks reach
 * only Socket Mode, so the connection's credential needs an app_token; without one the runner rejects.
 */
export async function runButtonApprovalApplication(project: LoadedProject, options: ButtonApprovalApplicationOptions, signal: AbortSignal): Promise<void> {
  const connection = newProjectConnection(project, ConnectionName, options.slackOptions);
  const flow = new ButtonApprovalFlow(connection);
  const startKey = { connectorId: ConnectorID, connectionName: ConnectionName, triggerName: ChannelThreadCreatedTriggerDefinition.trigger.triggerName, bindingName: StartTriggerBinding };
  const buttonKey = { connectorId: ConnectorID, connectionName: ConnectionName, triggerName: ButtonClickedTriggerDefinition.trigger.triggerName, bindingName: ButtonTriggerBinding };
  const startConfiguration = decodeTriggerConfiguration(project.configuration, startKey, ChannelThreadCreatedTriggerConfigurationSchema);
  const buttonConfiguration = decodeTriggerConfiguration(project.configuration, buttonKey, ButtonClickedTriggerConfigurationSchema);
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-slack-button-approval-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: options.serverAddress });
  const client = new Client(registry, cache, { serverAddress: options.serverAddress });
  const bindingLogger = (trigger: string, binding: string) => withTriggerLoggerAttributes(options.logger, { connector: ConnectorID, connection: ConnectionName, trigger, binding });
  // Both bindings share one runner, because Slack sends each Socket Mode envelope to only one connection.
  const runner = newProjectMessageTriggerRunner(project, ConnectionName, {
    channelThreadCreatedRoutes: [{ bindingName: StartTriggerBinding, target: newDexFlowTriggerTarget(client, flow, newStartTriggerFilter(startConfiguration), resolveFlowId, mapToFlowInput,
      { logger: bindingLogger(startKey.triggerName, StartTriggerBinding) }) }],
    buttonClickedRoutes: [{ bindingName: ButtonTriggerBinding, target: newDexRPCTriggerTarget(client, flow.receiveButtonDecision, newButtonTriggerFilter(buttonConfiguration), resolveButtonFlowId, mapToReceiveButtonDecisionInput,
      { logger: bindingLogger(buttonKey.triggerName, ButtonTriggerBinding) }) }],
  }, { ...options.slackOptions, logger: options.logger });
  const stop = new AbortController();
  const onAbort = () => stop.abort();
  signal.addEventListener('abort', onAbort, { once: true });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000, signal: stop.signal });
    options.onReady?.({ client, flow, worker });
    try { await runner.run(newTriggerContext(stop.signal)); }
    catch (error) { if (!(error instanceof TriggerDeliveryAbortedError && signal.aborted)) throw error; }
  } finally {
    signal.removeEventListener('abort', onAbort);
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}
