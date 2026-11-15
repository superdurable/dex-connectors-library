// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import type { Client } from '@superdurable/dex';
import type { TriggerLogLevel, TriggerLogger } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject, TriggerInbox, encodeTriggerEvent } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { decodeConnectMessage } from '../../src/index.js';
import { runEnvelopeOutcomeApplication } from '../../examples/envelope-outcome/app.js';
import { ConnectionName, EnvelopeOutcomeTriggerBinding, flowIdForRequest, type EnvelopeOutcomeFlow, type SigningState } from '../../examples/envelope-outcome/flow/workflow.js';
import { connectBody, connectSignature, deliverConnect, sentinelHMACKey, testAccountId } from '../trigger_support.js';

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};
const scope = { projectId: 'docusign-envelope-outcome', kind: 'live' } as const;
const inboxKey = { connectorId: 'docusign', connectionName: ConnectionName, triggerName: 'envelopeEventReceived', bindingName: EnvelopeOutcomeTriggerBinding };

/** A DocuSign stand-in for userinfo and envelope creation that numbers envelopes per create. */
function newDocuSignAPI() {
  const requests: string[] = [];
  const envelopeIds: string[] = [];
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    requests.push(`${request.method} ${url.pathname}`);
    const json = (status: number, body: unknown): ProviderResponse => ({ status, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(body)) });
    if (url.pathname === '/oauth/userinfo') return json(200, { accounts: [{ account_id: testAccountId, is_default: true, base_uri: 'https://na3.docusign.net' }] });
    if (request.method === 'POST' && url.pathname === `/restapi/v2.1/accounts/${testAccountId}/envelopes`) {
      const envelopeId = `93be49ab-1111-2222-3333-${randomUUID().replaceAll('-', '').slice(0, 12)}`;
      envelopeIds.push(envelopeId);
      return json(201, { envelopeId, status: 'sent', statusDateTime: '2026-10-04T12:00:00.0000000Z' });
    }
    return json(404, { errorCode: 'NOT_FOUND' });
  };
  return { send, requests, envelopeIds };
}

function newRecordingLogger(): TriggerLogger & { records: Array<{ level: TriggerLogLevel; message: string; attributes: Record<string, unknown> }> } {
  const records: Array<{ level: TriggerLogLevel; message: string; attributes: Record<string, unknown> }> = [];
  return { records, isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes: { ...attributes } }); } };
}

async function newProject(bindingConfiguration: Record<string, unknown>): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'docusign', connectionName: ConnectionName }, 0, new CredentialMaterial({
    auth_method: 'docusign-oauth', oauth_client_id: 'integration-key', oauth_client_secret: 'SENTINEL-CLIENT-SECRET', access_token: 'SENTINEL-ACCESS-TOKEN',
    refresh_token: 'SENTINEL-REFRESH-TOKEN', connect_hmac_key: sentinelHMACKey,
  }, new Date(Date.now() + 8 * 3_600_000), 'oauth2'));
  const project = new LoadedProject({ schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'docusign', connectionName: ConnectionName, modulePath: 'docusign', provider: 'docusign', configuration: {} }],
    triggerBindings: [{ ...inboxKey, configuration: bindingConfiguration }] }, undefined, objects, undefined, undefined);
  return { project, objects };
}

async function waitForStatus(client: Client, flow: EnvelopeOutcomeFlow, flowId: string, status: string): Promise<SigningState> {
  const deadline = Date.now() + 30_000;
  let last: unknown;
  for (;;) {
    try {
      const state = await client.invokeRPC(flow.getSigningState, flowId) as unknown as SigningState;
      last = state;
      if (state.status === status) return state;
    } catch (error) { last = error; }
    if (Date.now() > deadline) throw new Error(`Flow ${flowId} did not reach ${status}: ${String(last instanceof Error ? last.message : JSON.stringify(last))}`);
    await new Promise(resolve => setTimeout(resolve, 100));
  }
}

function startApplication(project: LoadedProject, send: ReturnType<typeof newDocuSignAPI>['send']) {
  const stop = new AbortController();
  const logger = newRecordingLogger();
  let resolveReady!: (ready: { client: Client; flow: EnvelopeOutcomeFlow; connectURL: string }) => void;
  const ready = new Promise<{ client: Client; flow: EnvelopeOutcomeFlow; connectURL: string }>(resolve => { resolveReady = resolve; });
  const running = runEnvelopeOutcomeApplication(project, { serverAddress: dexAddress(), connectHost: '127.0.0.1', connectPort: 0, logger, docuSignOptions: { send }, onReady: resolveReady }, stop.signal);
  return { stop, logger, ready, running };
}

const signedHeaders = (body: string, key = sentinelHMACKey) => ({ 'X-DocuSign-Signature-1': connectSignature(key, body) });
const request = (requestId: string) => ({ requestId, templateId: '8c9f5a8b-1111-2222-3333-4f5e6d7c8b9a', signers: [{ roleName: 'Customer', name: 'Priya Raman', email: 'priya@meridian.example.com' }], emailSubject: 'Meridian MSA' });

it('resumes the waiting Flow from a signed Connect outcome and refuses forged and uncorrelated deliveries', async () => {
  const { project, objects } = await newProject({});
  const docuSign = newDocuSignAPI();
  const application = startApplication(project, docuSign.send);
  try {
    const { client, flow, connectURL } = await application.ready;
    const requestId = `opp-${randomUUID().slice(0, 8)}`;
    const flowId = flowIdForRequest(requestId);
    await client.startFlow(flow, flowId, request(requestId));
    const waiting = await waitForStatus(client, flow, flowId, 'waitingForSignature');
    const envelopeId = waiting.envelopeId!;

    const completed = connectBody('envelope-completed', envelopeId, requestId);
    expect(await deliverConnect(connectURL, completed, signedHeaders(completed, 'forged-key')), 'a forged delivery is refused').toBe(400);
    const uncorrelated = connectBody('envelope-declined', envelopeId, 'opp-unknown').replace(',"customFields":{"textCustomFields":[{"fieldId":"1","name":"dexSigningRequestId","show":"false","value":"opp-unknown"}]}', '');
    expect(await deliverConnect(connectURL, uncorrelated, signedHeaders(uncorrelated)), 'an uncorrelated event is acknowledged').toBe(200);
    await expect.poll(() => application.logger.records.filter(record => record.message === 'trigger event skipped: filtered').length, { timeout: 30_000 }).toBe(1);
    expect((await waitForStatus(client, flow, flowId, 'waitingForSignature')).receivedEventIds).toEqual([]);

    expect(await deliverConnect(connectURL, completed, signedHeaders(completed))).toBe(200);
    expect(await deliverConnect(connectURL, completed, signedHeaders(completed)), 'a Connect retry is acknowledged again').toBe(200);
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SigningState>()).toEqual(expect.objectContaining({ status: 'completed', envelopeId, receivedEventIds: [`${envelopeId}:envelope-completed`] }));
    await expect.poll(async () => (await new TriggerInbox(objects, scope, inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
    expect(docuSign.envelopeIds).toEqual([envelopeId]);
    expect(JSON.stringify(application.logger.records)).not.toMatch(/SENTINEL/);
  } finally {
    application.stop.abort();
    await application.running;
  }
}, 120_000);

it('records only the configured outcomes and replays an event received before the Worker restarted', async () => {
  const { project, objects } = await newProject({ events: ['envelope-declined'] });
  const docuSign = newDocuSignAPI();
  const first = startApplication(project, docuSign.send);
  const requestId = `opp-${randomUUID().slice(0, 8)}`;
  const flowId = flowIdForRequest(requestId);
  let envelopeId: string;
  try {
    const { client, flow, connectURL } = await first.ready;
    await client.startFlow(flow, flowId, request(requestId));
    envelopeId = (await waitForStatus(client, flow, flowId, 'waitingForSignature')).envelopeId!;
    const voided = connectBody('envelope-voided', envelopeId, requestId);
    expect(await deliverConnect(connectURL, voided, signedHeaders(voided)), 'a filtered outcome is acknowledged unrecorded').toBe(200);
    expect(await new TriggerInbox(objects, scope, inboxKey).pending()).toEqual([]);
  } finally {
    first.stop.abort();
    await first.running;
  }
  // The previous process recorded and acknowledged a decline but stopped before delivering it.
  const declined = decodeConnectMessage(Buffer.from(connectBody('envelope-declined', envelopeId, requestId)), new Date())!;
  await new TriggerInbox(objects, scope, inboxKey).add({ id: declined.id, event: encodeTriggerEvent(declined) });
  const second = startApplication(project, docuSign.send);
  try {
    const { client } = await second.ready;
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SigningState>()).toEqual(expect.objectContaining({ status: 'declined', receivedEventIds: [`${envelopeId}:envelope-declined`] }));
    await expect.poll(async () => (await new TriggerInbox(objects, scope, inboxKey).pending()).length, { timeout: 30_000 }).toBe(0);
  } finally {
    second.stop.abort();
    await second.running;
  }
}, 120_000);
