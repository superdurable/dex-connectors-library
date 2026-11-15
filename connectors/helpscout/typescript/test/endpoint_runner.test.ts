// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type Call, type CredentialRefreshDriver, type RefreshingCredentialProvider, type TriggerTarget } from '@superdurable/connectors-sdk';
import { InMemoryObjectStore, ObjectNotFoundError, TriggerInbox, type Configuration, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, describe, expect, it } from 'vitest';
import { HelpScoutClient } from '../src/client.js';
import { WebhookEventConversationCreated, conversationEventWebhookHandler, newConnection, newConversationEventTrigger, type ConversationEvent, type Credentials } from '../src/index.js';
import { newConversationEventEndpointRunnerWithInboxes, type ConversationEventEndpointRunner } from '../src/trigger.js';
import { LoopbackServers, RecordingTarget, conversationJSON, deliver, eventually, fixedNow, newTestConnection, sentinelWebhookSecret, testConnection } from './trigger_support.js';

const scope = { projectId: 'helpscout-endpoint-runner-tests', kind: 'live' as const };
const conversationsInboxKey: TriggerInboxKey = { connectorId: 'helpscout', connectionName: testConnection.name, triggerName: 'conversationEvent', bindingName: 'conversations' };

/** The project configuration Dex Web saves for the conversations binding. */
function conversationsConfiguration(bindingConfiguration: Record<string, unknown> | undefined): Configuration {
  return {
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha3', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'helpscout', connectionName: testConnection.name, modulePath: 'helpscout', provider: 'helpscout', configuration: {} }],
    triggerBindings: bindingConfiguration ? [{ ...conversationsInboxKey, configuration: bindingConfiguration }] : [],
  } as Configuration;
}

const servers = new LoopbackServers();
afterEach(async () => { await servers.closeAll(); });

/** One endpoint runner on in-memory project storage that runs until stop. */
async function startEndpointRunner(objects: InMemoryObjectStore, configuration: Configuration, target: TriggerTarget<ConversationEvent>) {
  const runner: ConversationEventEndpointRunner = newConversationEventEndpointRunnerWithInboxes(newTestConnection(sentinelWebhookSecret), configuration,
    [{ bindingName: conversationsInboxKey.bindingName, target }], key => new TriggerInbox(objects, scope, key));
  const abort = new AbortController();
  const result = runner.run(newTriggerContext(abort.signal));
  await eventually(() => runner.runningSourceCount() === 1, 'the endpoint runner did not start receiving');
  const url = await servers.serve((request, response) => runner.serveHTTP(request, response));
  return { url, stop: async () => { abort.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
}

async function pendingEventIds(objects: InMemoryObjectStore): Promise<string[]> {
  return (await new TriggerInbox(objects, scope, conversationsInboxKey).pending()).map(event => event.id);
}

describe('project conversationEvent endpoint runner', () => {
  it('stores each event before acknowledging it and replays it after a restart', async () => {
    const objects = new InMemoryObjectStore();
    const configuration = conversationsConfiguration({ mailboxId: 123 });
    const firstRun = await startEndpointRunner(objects, configuration, new RecordingTarget(() => new Error('the Dex Server is unreachable')));
    expect((await deliver(firstRun.url, WebhookEventConversationCreated, conversationJSON(501, 'active', 123), sentinelWebhookSecret)).status).toBe(200);
    const pending = await pendingEventIds(objects);
    expect(pending, 'the 200 came after the inbox write').toHaveLength(1);
    expect(pending[0]).toMatch(/^convo\.created:501:[0-9a-f]{32}$/);
    expect((await deliver(firstRun.url, WebhookEventConversationCreated, conversationJSON(502, 'active', 456), sentinelWebhookSecret)).status).toBe(200);
    expect(await pendingEventIds(objects)).toEqual(pending);
    await firstRun.stop();
    expect(await pendingEventIds(objects), 'an undelivered event survives the restart').toEqual(pending);

    const delivered = new RecordingTarget();
    const secondRun = await startEndpointRunner(objects, configuration, delivered);
    const replayed = await delivered.eventAt(0);
    expect(replayed.id).toBe(pending[0]);
    expect(replayed.occurredAt).toEqual(fixedNow);
    expect(replayed.payload.conversation.primaryCustomer?.email).toBe('jane@acme.example.com');
    await eventually(async () => (await pendingEventIds(objects)).length === 0, 'the replayed event stayed pending');
    await secondRun.stop();
  });

  it('requires each route binding in the project configuration', () => {
    const objects = new InMemoryObjectStore();
    const target = new RecordingTarget();
    const open = (key: TriggerInboxKey) => new TriggerInbox(objects, scope, key);
    const build = (configuration: Configuration, bindingNames = ['conversations']) => newConversationEventEndpointRunnerWithInboxes(newTestConnection(sentinelWebhookSecret),
      configuration, bindingNames.map(bindingName => ({ bindingName, target })), open);
    let failure: unknown;
    try { build(conversationsConfiguration(undefined)); } catch (error) { failure = error; }
    expect((failure as Error).cause).toBeInstanceOf(ObjectNotFoundError);
    expect((failure as Error).message).toContain('binding "conversations"');
    expect(() => build(conversationsConfiguration({ mailboxId: -1 }))).toThrow(/binding "conversations"/);
    expect(() => build(conversationsConfiguration({ inbox: 123 }))).toThrow(/binding "conversations" configuration/);
    expect(() => build(conversationsConfiguration({ events: null, mailboxId: null })), 'a cleared picker accepts every inbox, as in Go').not.toThrow();
    expect(() => build(conversationsConfiguration({}), [])).toThrow(/routes are required/);
    expect(() => build(conversationsConfiguration({}), ['conversations', ' conversations '])).toThrow(/duplicated/);
    expect(() => build(conversationsConfiguration({}), [' '])).toThrow(/binding name and target are required/);
  });
});

/** A refreshing provider that records each refresh decision the endpoint asks of its driver. */
class DecisionRecordingProvider implements RefreshingCredentialProvider<Credentials> {
  readonly decisions: boolean[] = [];
  constructor(private readonly credentials: Credentials, private readonly expiresAt: Date | undefined) {}
  async resolve(): Promise<Credentials> { return this.credentials; }
  async resolveWithRefresh(_call: Call, driver: CredentialRefreshDriver<Credentials>): Promise<Credentials> {
    this.decisions.push(driver.refreshRequired({ credentials: this.credentials, now: fixedNow, ...(this.expiresAt ? { expiresAt: this.expiresAt } : {}) }));
    return this.credentials;
  }
}

describe('webhook credential resolution', () => {
  it('asks for a token refresh only for an expired record, never for a blank or near-expiry token', async () => {
    for (const [expiresAt, isRefreshRequired] of [
      [undefined, false], [new Date(fixedNow.getTime() + 60_000), false], [fixedNow, true], [new Date(fixedNow.getTime() - 1), true],
    ] as const) {
      const provider = new DecisionRecordingProvider({ appId: 'inert-app-id', appSecret: new SecretString('inert-app-secret'), webhookSecret: new SecretString(sentinelWebhookSecret) }, expiresAt);
      const connection = newConnection(new HelpScoutClient({}, provider, { now: () => fixedNow }), testConnection);
      const handler = conversationEventWebhookHandler(connection);
      const abort = new AbortController();
      const result = newConversationEventTrigger({ connection, connectionName: testConnection.name, bindingName: 'conversations', target: new RecordingTarget() }).run(newTriggerContext(abort.signal));
      await eventually(() => handler.runningSourceCount() === 1, 'the binding did not start receiving');
      const url = await servers.serve((request, response) => handler.serveHTTP(request, response));
      expect((await deliver(url, WebhookEventConversationCreated, conversationJSON(501, 'active', 123), sentinelWebhookSecret)).status).toBe(200);
      expect(provider.decisions).toEqual([isRefreshRequired]);
      abort.abort();
      await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  });
});
