// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GuGuDataClient } from '../src/client.js';
import { SegmentTextInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gugudata/scenarios.json', import.meta.url));
const connection = { provider: 'gugudata', name: 'gugudata' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGuGuData, ''));

async function invokeGuGuData(scenario: Scenario, recorder: ExchangeRecorder) {
  const appKey = scenario.name.endsWith('missing-credentials') ? '' : 'test-gugudata-app-key';
  const credentials: Array<[typeof connection, { appKey: SecretString }]> = [[connection, { appKey: new SecretString(appKey) }]];
  const client = new GuGuDataClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'gugudata-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'gugudata', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'segmentText') return client.segmentText().invoke(call, SegmentTextInputSchema.parse(scenario.input));
  throw new Error('unknown GuGuData operation ' + scenario.operation);
}
