// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { SnowflakeClient } from '../src/client.js';
import { SubmitStatementInputSchema, GetStatementResultInputSchema, CancelStatementInputSchema, type SubmitStatementInput, type CancelStatementInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/snowflake/scenarios.json', import.meta.url));
const connection = { provider: 'snowflake', name: 'warehouse' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSnowflake));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeSnowflake(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics', warehouse: 'REPORTING_WH', role: 'DEX_REPORTING', database: 'ANALYTICS', schema: 'BILLING' }, {
    resolve: async () => ({ authMethodId: 'programmatic-access-token', programmaticAccessToken: new SecretString('pat-secret-value-0123456789') }),
  }, { localProviderUrl: 'http://127.0.0.1', send: sender });
  const call = scenarioCall(scenario, connection, 'snowflake', {
    flowId: 'snowflake-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'submitStatement') return productionMutation(client.submitStatement(), call, SubmitStatementInputSchema.parse(scenario.input) as SubmitStatementInput, scenario.idempotencyKey!);
  if (scenario.operation === 'getStatementResult') return client.getStatementResult().invoke(call, GetStatementResultInputSchema.parse(scenario.input));
  if (scenario.operation === 'cancelStatement') return productionMutation(client.cancelStatement(), call, CancelStatementInputSchema.parse(scenario.input) as CancelStatementInput, scenario.idempotencyKey!);
  throw new Error(`unknown Snowflake operation ${scenario.operation}`);
}
