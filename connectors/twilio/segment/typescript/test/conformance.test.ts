// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SegmentClient } from '../src/client.js';
import { GetSourceInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/twilio/segment/scenarios.json', import.meta.url));
const connection = { provider: 'segment', name: 'segment' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSegment, '/api'));

async function invokeSegment(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-segment-api-key') }]];
  const client = new SegmentClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'segment-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'segment', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getSource') return client.getSource().invoke(call, GetSourceInputSchema.parse(scenario.input));
  throw new Error(`unknown Segment operation ${scenario.operation}`);
}
