// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { TuyaClient } from '../src/client.js';
import { GetDeviceDetailsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/tuya/scenarios.json', import.meta.url));
const connection = { provider: 'tuya', name: 'tuya' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTuya, ''));

async function invokeTuya(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientId: SecretString; clientSecret: SecretString }]> = [[connection, { clientId: new SecretString('sample-client-id'), clientSecret: new SecretString('sample-client-secret') }]];
  const client = new TuyaClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    now: () => new Date(scenario.fixedClock || '2026-01-02T03:04:05Z'),
    nonce: syntheticNonce(scenario.randomSeed || ''),
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'tuya-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'tuya', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getDeviceDetails') return client.getDeviceDetails().invoke(call, GetDeviceDetailsInputSchema.parse(scenario.input));
  throw new Error(`unknown Tuya operation ${scenario.operation}`);
}

function syntheticNonce(seed: string): () => Uint8Array {
  let counter = 0n;
  let block = Buffer.alloc(0);
  let offset = 0;
  return () => {
    const result = Buffer.alloc(16);
    for (let index = 0; index < result.length; index++) {
      if (offset === block.length) {
        const counterBytes = Buffer.alloc(8);
        counterBytes.writeBigUInt64BE(counter++);
        block = createHash('sha256').update(seed).update(counterBytes).digest();
        offset = 0;
      }
      result[index] = block[offset++] as number;
    }
    return result;
  };
}
