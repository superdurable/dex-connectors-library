// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FeihanClient } from '../src/client.js';
import { QueryRechargeOrderInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/feihan/scenarios.json', import.meta.url));
const connection = { provider: 'feihan', name: 'feihan' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFeihan, ''));

async function invokeFeihan(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { projectId: string; password: SecretString; keyStr: SecretString }]> = [[connection, { projectId: 'A12345', password: new SecretString('project-password'), keyStr: new SecretString('private-key') }]];
  const client = new FeihanClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'feihan-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'feihan', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'queryRechargeOrder') return client.queryRechargeOrder().invoke(call, QueryRechargeOrderInputSchema.parse(scenario.input));
  throw new Error('unknown Feihan operation ' + scenario.operation);
}
