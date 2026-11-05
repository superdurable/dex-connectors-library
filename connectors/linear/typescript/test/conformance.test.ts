// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { LinearClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/linear/scenarios.json', import.meta.url));
const connection = { provider: 'linear', name: 'linear-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLinear));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeLinear(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider<Credentials>([[connection, { authMethodId: 'personal-api-key', apiKey: new SecretString('linear-conformance-key') }]]);
  const client = new LinearClient({}, credentials, { endpoint: 'http://127.0.0.1:43201/graphql', send });
  const call = scenarioCall(scenario, connection, 'linear', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  const input = scenario.input as never;
  switch (scenario.operation) {
    case 'searchIssues': return client.searchIssues().invoke(call, input);
    case 'getIssue': return client.getIssue().invoke(call, input);
    case 'createIssue': return productionMutation(client.createIssue(), call, input, scenario.idempotencyKey!);
    case 'updateIssue': return productionMutation(client.updateIssue(), call, input, scenario.idempotencyKey!);
    case 'addComment': return productionMutation(client.addComment(), call, input, scenario.idempotencyKey!);
    case 'findUserByEmail': return client.findUserByEmail().invoke(call, input);
    case 'listWorkflowStates': return client.listWorkflowStates().invoke(call, input);
    default: throw new Error(`unknown Linear operation ${scenario.operation}`);
  }
}
