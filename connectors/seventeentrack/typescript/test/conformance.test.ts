// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SeventeenTrackClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/seventeentrack/scenarios.json', import.meta.url));
const connection = { provider: 'seventeentrack', name: 'seventeentrack' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSeventeenTrack, ''));

async function invokeSeventeenTrack(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { apiKey: new SecretString('secretTestKey') }]]);
  const client = new SeventeenTrackClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'seventeentrack-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'seventeentrack', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listTrackedShipments') return client.listTrackedShipments().invoke(call, scenario.input as unknown as Parameters<ReturnType<SeventeenTrackClient['listTrackedShipments']>['invoke']>[1]);
  throw new Error(`unknown 17TRACK operation ${scenario.operation}`);
}
