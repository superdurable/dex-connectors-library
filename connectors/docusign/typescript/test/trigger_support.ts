// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { once } from 'node:events';
import { createServer, type Server } from 'node:http';
import { SecretString, StaticCredentialProvider, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { DocuSignClient } from '../src/client.js';
import { newConnection, type Connection, type Credentials, type EnvelopeEvent } from '../src/index.js';

/** The connection every Trigger test uses. */
export const testConnection = { provider: 'docusign', name: 'docusign-esignature' };
/** Sender account of every Connect message. */
export const testAccountId = 'a4ec37d6-1111-2222-3333-143885c220e1';
/** Envelope of every Connect message. */
export const testEnvelopeId = '93be49ab-1111-2222-3333-f752070d71ec';
/** Connect HMAC key that must never reach a record. */
export const sentinelHMACKey = 'SENTINEL-CONNECT-HMAC-KEY';

/** Credentials with an inert access token and the sentinel Connect key. */
export function testCredentials(connectHmacKey = sentinelHMACKey): Credentials {
  return {
    authMethodId: 'docusign-oauth', oauthClientId: 'integration-key', oauthClientSecret: new SecretString('inert-client-secret'),
    accessToken: new SecretString('inert-access-token'), refreshToken: new SecretString('inert-refresh-token'), connectHmacKey: new SecretString(connectHmacKey),
  };
}

/** A DocuSign connection whose client holds credentials and accepts connectMaxBodyBytes. */
export function newTestConnection(credentials: Credentials = testCredentials(), connectMaxBodyBytes = 4096): Connection {
  return newConnection(new DocuSignClient({ connectMaxBodyBytes }, new StaticCredentialProvider([[testConnection, credentials]])), testConnection);
}

/** A JSON SIM envelope event with an envelope summary that includes custom fields. */
export function connectBody(event: string, envelopeId: string, requestId: string): string {
  return `{"event":"${event}","apiVersion":"v2.1","uri":"/restapi/v2.1/accounts/${testAccountId}/envelopes/${envelopeId}",`
    + '"retryCount":0,"configurationId":10242474,"generatedDateTime":"2026-10-04T13:00:00.1230000Z",'
    + `"data":{"accountId":"${testAccountId}","userId":"4799e5e9-0000-0000-0000-cf4713bbcacc","envelopeId":"${envelopeId}",`
    + `"envelopeSummary":{"status":"${event.replace(/^envelope-/, '')}","emailSubject":"Meridian MSA",`
    + '"voidedReason":"Signer left the company","customFields":{"textCustomFields":['
    + `{"fieldId":"1","name":"dexSigningRequestId","show":"false","value":"${requestId}"}]}}}}`;
}

/** The base64 HMAC-SHA256 DocuSign sends in X-DocuSign-Signature-N. */
export function connectSignature(key: string, body: string): string {
  return createHmac('sha256', key).update(body).digest('base64');
}

/** A target that records every delivered event. */
export class CollectingTarget implements TriggerTarget<EnvelopeEvent> {
  readonly events: Array<TriggerEvent<EnvelopeEvent>> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<EnvelopeEvent>): Promise<void> { this.events.push(event); }
}

/** Serve a handler on a loopback port and return its URL. */
export async function serveConnect(handler: { serveHTTP(request: import('node:http').IncomingMessage, response: import('node:http').ServerResponse): Promise<void> }, servers: Server[]): Promise<string> {
  const server = createServer((request, response) => { void handler.serveHTTP(request, response); });
  servers.push(server);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('server address unavailable');
  return `http://127.0.0.1:${address.port}/docusign/connect`;
}

/** POST one Connect delivery and return its status. */
export async function deliverConnect(url: string, body: string, headers: Record<string, string> = {}): Promise<number> {
  const response = await fetch(url, { method: 'POST', body, headers: { 'Content-Type': 'application/json', ...headers } });
  await response.arrayBuffer();
  return response.status;
}
