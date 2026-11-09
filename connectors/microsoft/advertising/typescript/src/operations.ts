// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ListCampaignsDefinition } from './generated/definitions.js';
import type { CampaignSummary, CampaignsPage, ListCampaignsInput } from './generated/models.js';
import type { MicrosoftAdvertisingClient, MicrosoftAdvertisingExchange } from './client.js';

const advertisingIDPattern = /^[1-9][0-9]{0,18}$/;
const maximumSignedID = 9_223_372_036_854_775_807n;
const emptyPage = (): CampaignsPage => ({ campaigns: [] });

/** Check that an account or campaign ID fits Microsoft's signed 64-bit long. */
export function validAdvertisingID(value: unknown): value is string {
  return typeof value === 'string' && advertisingIDPattern.test(value) && BigInt(value) <= maximumSignedID;
}

/** List campaign IDs, names, types, and statuses in one account. */
export class ListCampaignsOperation implements Query<ListCampaignsInput, CampaignsPage> {
  /** Bind the provider client. */ constructor(private readonly client: MicrosoftAdvertisingClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListCampaignsDefinition; }
  /** Query the account and project only selected campaign fields. */
  async invoke(call: Call, input: ListCampaignsInput): Promise<QueryAttempt<CampaignsPage>> {
    if (!input || !validAdvertisingID(input.accountId))
      return branch('defect', 'VALIDATION', 'Microsoft Advertising account ID is invalid');
    const exchange = await this.client.postCampaigns(call, input.accountId);
    const failure = classifyRead(exchange);
    if (failure) return failure;
    const page = decodePage(exchange.body);
    if (!page) return branch('invalidResponse', 'PROTOCOL', 'Microsoft Advertising returned invalid campaign JSON');
    return newQueryBranch('listed', page);
  }
}

function classifyRead(exchange: MicrosoftAdvertisingExchange): QueryAttempt<CampaignsPage> | undefined {
  if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'Microsoft Advertising connection credentials are unavailable');
  if (exchange.error === 'refresh' || exchange.error === 'request') return retry('TRANSPORT', 'Microsoft Advertising campaign request failed');
  if (exchange.status === 429) return retry('RATE_LIMIT', 'Microsoft Advertising rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Microsoft Advertising is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Microsoft Advertising rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Microsoft Advertising campaign response exceeded the configured limit');
  return undefined;
}
function decodePage(body: Uint8Array): CampaignsPage | undefined {
  let value: unknown;
  try { value = JSON.parse(Buffer.from(body).toString('utf8')); } catch { return undefined; }
  if (!isObject(value) || !Array.isArray(value.Campaigns)) return undefined;
  const campaigns: CampaignSummary[] = [];
  for (const item of value.Campaigns) {
    if (!isObject(item) || !validAdvertisingID(item.Id) ||
      typeof item.Name !== 'string' || !item.Name ||
      typeof item.CampaignType !== 'string' || !item.CampaignType ||
      typeof item.Status !== 'string' || !item.Status) return undefined;
    campaigns.push({ id: item.Id, name: item.Name, campaignType: item.CampaignType, status: item.Status });
  }
  return { campaigns };
}
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
function branch(branchId: string, kind: Failure['kind'], message: string): QueryAttempt<CampaignsPage> {
  return newQueryBranch(branchId, emptyPage(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<CampaignsPage> {
  return newQueryRetry(failure(kind, message), delay);
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'microsoft-advertising', operation: 'listCampaigns', message };
}
