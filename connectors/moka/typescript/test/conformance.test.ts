// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MokaClient } from '../src/client.js';
import { ListJobFunctionsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/moka/scenarios.json', import.meta.url));
const connection = { provider: 'moka', name: 'moka' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMoka, ''));

async function invokeMoka(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('defect') ? '' : 'secret-api-key';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new MokaClient({ orgId: 'orgid-test' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'moka-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'moka', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listJobFunctions().invoke(call, ListJobFunctionsInputSchema.parse(scenario.input));
}
