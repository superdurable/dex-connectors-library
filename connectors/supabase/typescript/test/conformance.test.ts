// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { SupabaseClient } from '../src/client.js';
import { SelectRowsInputSchema, UpsertRowsInputSchema, type UpsertRowsInput, type UpsertRowsOutput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/supabase/scenarios.json', import.meta.url));
const connection = { provider: 'supabase', name: 'supabase' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSupabase));

async function invokeSupabase(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new SupabaseClient({ baseUrl: 'http://127.0.0.1:12345/rest/v1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-token') }),
  }, { send });
  const call = scenarioCall(scenario, connection, 'supabase', {
    flowId: 'provider-test', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'selectRows') return client.selectRows().invoke(call, SelectRowsInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertRows') {
    const operation = client.upsertRows();
    return invokeProductionMutation(operation, scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<UpsertRowsInput, UpsertRowsOutput>, call, UpsertRowsInputSchema.parse(scenario.input)),
      error => error instanceof RetryError);
  }
  throw new Error(`unknown operation ${scenario.operation}`);
}
