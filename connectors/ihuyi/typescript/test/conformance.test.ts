// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { IhuyiClient } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ihuyi/scenarios.json', import.meta.url));
const connection = { provider: 'ihuyi', name: 'ihuyi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeIhuyi, ''));

async function invokeIhuyi(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiId: 'test-account', apiKey: new SecretString('private-test-key') }]]);
  const client = new IhuyiClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 65_536 }, credentials, {
    now: () => new Date('2026-01-01T00:00:00Z'),
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'ihuyi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ihuyi', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'lookupIPLocation') return client.lookupIPLocation().invoke(call, LookupIPLocationInputSchema.parse(scenario.input));
  throw new Error('unknown Ihuyi operation ' + scenario.operation);
}
