// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { generateKeyPairSync, createHash, verify } from 'node:crypto';
import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { LianlianpayClient, signPaymentRequest } from '../src/client.js';
import { QueryPaymentStatusInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/lianlianpay/scenarios.json', import.meta.url));
const connection = { provider: 'lianlianpay', name: 'lianlianpay' };
const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 1024 });
const privateKeyPEM = privateKey.export({ format: 'pem', type: 'pkcs1' }).toString();
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLianlianpay, ''));
it('signs the second MD5 digest with an RSA private key', () => {
  const body = new TextEncoder().encode('{"mch_id":"2020042200284052","txn_seqno":"ORDER123"}');
  const signature = signPaymentRequest(body, privateKey);
  const firstHex = createHash('md5').update(body).digest('hex');
  expect(verify('md5', Buffer.from(firstHex), publicKey, Buffer.from(signature, 'base64'))).toBe(true);
});

async function invokeLianlianpay(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { merchantId: '2020042200284052', privateKey: new SecretString(privateKeyPEM) }]]);
  const signature = scenario.exchanges[0]?.requestHeaders?.['Signature-Data']?.[0] ?? '';
  const client = new LianlianpayClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 16 : 262_144 }, credentials, {
    now: () => new Date('2026-01-01T00:00:00Z'), sign: () => signature,
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'lianlianpay-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'lianlianpay', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'queryPaymentStatus') return client.queryPaymentStatus().invoke(call, QueryPaymentStatusInputSchema.parse(scenario.input));
  throw new Error('unknown Lianlianpay operation ' + scenario.operation);
}
