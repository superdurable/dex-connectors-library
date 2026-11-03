// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { BambooHRClient } from '../src/client.js';
import { AddEmployeeInputSchema, FindEmployeeByEmailInputSchema, GetEmployeeInputSchema, ListEmployeeChangesInputSchema, ListTimeOffRequestsInputSchema, UpdateEmployeeInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/bamboohr/scenarios.json', import.meta.url));
const connection = { provider: 'bamboohr', name: 'bamboohr-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBambooHR, '/api/v1'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeBambooHR(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new BambooHRClient({ companyDomain: 'acme' }, {
    resolve: async () => ({ apiKey: new SecretString('0123456789abcdef' + '0123456789abcdef01234567') }),
  }, { apiBaseURL: 'http://127.0.0.1:12345/api/v1', send: sender });
  const call = scenarioCall(scenario, connection, 'bamboohr', context) as Call;
  if (scenario.operation === 'getEmployee') return client.getEmployee().invoke(call, GetEmployeeInputSchema.parse(scenario.input));
  if (scenario.operation === 'findEmployeeByEmail') return client.findEmployeeByEmail().invoke(call, FindEmployeeByEmailInputSchema.parse(scenario.input));
  if (scenario.operation === 'listEmployeeChanges') return client.listEmployeeChanges().invoke(call, ListEmployeeChangesInputSchema.parse(scenario.input));
  if (scenario.operation === 'listTimeOffRequests') return client.listTimeOffRequests().invoke(call, ListTimeOffRequestsInputSchema.parse(scenario.input));
  if (scenario.operation === 'updateEmployee') return productionMutation(client.updateEmployee(), call, UpdateEmployeeInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addEmployee') return productionMutation(client.addEmployee(), call, AddEmployeeInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown BambooHR operation ${scenario.operation}`);
}
