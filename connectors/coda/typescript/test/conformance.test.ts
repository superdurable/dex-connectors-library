// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { jsonCodec } from '@superdurable/dex';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CodaClient } from '../src/client.js';
import { GetRowInputSchema, ListRowsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/coda/scenarios.json', import.meta.url));
const connection = { provider: 'coda', name: 'coda' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCoda));

it.each(scenarios.filter(scenario => scenario.name.endsWith('-large-integer')))(
  'preserves the provider integer in the encoded output: $name', async scenario => {
    const precisionScenario = structuredClone(scenario);
    const exchange = precisionScenario.exchanges[0];
    if (!exchange?.body) throw new Error('large-integer scenario needs a response body');
    exchange.body = exchange.body.replace('9007199254740994', '9007199254740993');
    const recorder = new ExchangeRecorder(precisionScenario);
    const attempt = await invokeCoda(precisionScenario, recorder);
    recorder.finish();
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected a successful Coda response');
    const encoded = jsonCodec().encode(attempt.value);
    expect(encoded.kind).toBe('json');
    expect(encoded.data).toContain('9007199254740993');
    expect(encoded.data).not.toContain('9007199254740992');
  },
);

async function invokeCoda(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new CodaClient({ baseUrl: 'http://127.0.0.1:12345/apis/v1' }, {
    resolve: async () => ({ apiToken: new SecretString('test-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'coda', {
    flowId: 'coda-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listRows') return client.listRows().invoke(call, ListRowsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getRow') return client.getRow().invoke(call, GetRowInputSchema.parse(scenario.input));
  throw new Error(`unknown Coda operation ${scenario.operation}`);
}
