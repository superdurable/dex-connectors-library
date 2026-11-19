// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { CloopenClient } from '../src/client.js';
import { GetBalanceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/cloopen/scenarios.json', import.meta.url));
const connection = { provider: 'cloopen', name: 'cloopen' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCloopen, ''));

async function invokeCloopen(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = scenario.name === 'getBalance-missing-credentials' ? { accountSid: new SecretString(''), authToken: new SecretString('') }
    : { accountSid: new SecretString('abcdefghijklmnopqrstuvwxyz012345'), authToken: new SecretString('012345abcdefghijklmnopqrstuvwxyz') };
  const client = new CloopenClient({ baseUrl: 'http://127.0.0.1:12345', ...(scenario.name === 'getBalance-oversize' ? { maxResponseBytes: 4 } : {}) },
    new StaticCredentialProvider([[connection, credentials]]), { now: () => new Date(scenario.fixedClock!), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'cloopen-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'cloopen', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getBalance') return client.getBalance().invoke(call, GetBalanceInputSchema.parse(scenario.input));
  throw new Error(`unknown Cloopen operation ${scenario.operation}`);
}
