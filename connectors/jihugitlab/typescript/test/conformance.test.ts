// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JiHuGitLabClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/jihugitlab/scenarios.json', import.meta.url));
const connection = { provider: 'jihugitlab', name: 'jihugitlab' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJiHuGitLab));

async function invokeJiHuGitLab(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listMemberProjects') throw new Error(`unknown JiHuGitLab operation ${scenario.operation}`);
  const client = new JiHuGitLabClient({}, new StaticCredentialProvider([[connection, { apiKey: new SecretString(scenario.name.endsWith('defect') ? '' : 'private-test-key') }]]), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'jihugitlab-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'jihugitlab', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listMemberProjects().invoke(call, scenario.input as { page: number; perPage: number });
}
