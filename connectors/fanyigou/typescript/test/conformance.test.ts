// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FanyigouClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/fanyigou/scenarios.json', import.meta.url));
const connection = { provider: 'fanyigou', name: 'fanyigou' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFanyigou, ''));

async function invokeFanyigou(scenario: Scenario, recorder: ExchangeRecorder) {
  const appID = scenario.name.endsWith('defect') ? '' : 'test-app';
  const credentials = new StaticCredentialProvider([[connection, { appID: new SecretString(appID), privateKey: new SecretString('test-private-key') }]]);
  const nonceHex = scenario.exchanges[0]?.form?.nonce_str?.[0] ?? '00000000000000000000000000000000';
  const client = new FanyigouClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1_048_576 }, credentials, {
    nonce: () => Buffer.from(nonceHex, 'hex'),
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'fanyigou-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'fanyigou', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAccountBalance') return client.getAccountBalance().invoke(call);
  throw new Error('unknown Fanyigou operation ' + scenario.operation);
}
