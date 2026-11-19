// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { IPPlus360Client } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ipplus360/scenarios.json', import.meta.url));
const connection = { provider: 'ipplus360', name: 'ipplus360' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeIPPlus360));

async function invokeIPPlus360(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'lookupIPLocation') throw new Error(`unknown IPPlus360 operation ${scenario.operation}`);
  const client = new IPPlus360Client({}, new StaticCredentialProvider([[connection, { apiKey: new SecretString('private-test-key') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'ipplus360-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'ipplus360', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.lookupIPLocation().invoke(call, LookupIPLocationInputSchema.parse(scenario.input));
}
