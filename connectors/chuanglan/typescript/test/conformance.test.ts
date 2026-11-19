// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ChuanglanClient } from '../src/client.js';
import { GetBalanceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/chuanglan/scenarios.json', import.meta.url));
const connection = { provider: 'chuanglan', name: 'chuanglan' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeChuanglan, ''));

async function invokeChuanglan(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = scenario.name === 'getBalance-missing-credentials' ? { account: new SecretString(''), password: new SecretString('') }
    : { account: new SecretString('N9000123'), password: new SecretString('secret-password') };
  const client = new ChuanglanClient({ baseUrl: 'http://127.0.0.1:12345', ...(scenario.name === 'getBalance-oversize' ? { maxResponseBytes: 4 } : {}) },
    new StaticCredentialProvider([[connection, credentials]]), { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'chuanglan-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'chuanglan', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getBalance') return client.getBalance().invoke(call, GetBalanceInputSchema.parse(scenario.input));
  throw new Error(`unknown Chuanglan operation ${scenario.operation}`);
}
