// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { NetlifyClient } from '../src/client.js';
import { ListDeploysInputSchema, ListSitesInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/netlify/scenarios.json', import.meta.url));
const connection = { provider: 'netlify', name: 'netlify' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNetlify));

async function invokeNetlify(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new NetlifyClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async (call: Call) => {
      if (call.id === 'netlify-sites-defect') throw new Error('credentials unavailable');
      return { accessToken: new SecretString('test-accesstoken') };
    },
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'netlify', {
    flowId: 'netlify-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listSites') return client.listSites().invoke(call, ListSitesInputSchema.parse(scenario.input));
  if (scenario.operation === 'listDeploys') return client.listDeploys().invoke(call, ListDeploysInputSchema.parse(scenario.input));
  throw new Error(`unknown Netlify operation ${scenario.operation}`);
}
