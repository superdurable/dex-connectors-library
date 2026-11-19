// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JinshujuClient } from '../src/client.js';
import { ListFormsPageInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jinshuju/scenarios.json', import.meta.url));
const connection = { provider: 'jinshuju', name: 'jinshuju' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJinshuju, ''));

async function invokeJinshuju(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString(scenario.name.endsWith('-defect') ? '' : 'access-secret') }]];
  const client = new JinshujuClient({ maxResponseBytes: 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'jinshuju-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'jinshuju', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listFormsPage') return client.listFormsPage().invoke(call, ListFormsPageInputSchema.parse(scenario.input));
  throw new Error('unknown Jinshuju operation ' + scenario.operation);
}
