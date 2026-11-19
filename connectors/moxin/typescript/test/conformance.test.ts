// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MoxinClient } from '../src/client.js';
import { ListModelsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/moxin/scenarios.json', import.meta.url));
const connection = { provider: 'moxin', name: 'moxin' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMoxin, ''));

async function invokeMoxin(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials = new StaticCredentialProvider([[connection, { platformToken: new SecretString(scenario.name.endsWith("defect") ? "" : "sk-test-token") }]]);
  const client = new MoxinClient({}, credentials, {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'moxin-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'moxin', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listModels().invoke(call, ListModelsInputSchema.parse(scenario.input));
}
