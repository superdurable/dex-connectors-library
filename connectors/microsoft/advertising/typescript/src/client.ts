// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, type Call, type CredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';
import { ListCampaignsOperation, validAdvertisingID } from './operations.js';

const campaignsPath = '/CampaignManagement/v13/Campaigns/QueryByAccountId';
const allCampaignTypes = 'App Audience Hotel ObjectiveBased PerformanceMax Search Shopping';

/** Injectable Microsoft Advertising HTTP and OAuth dependencies. */
export interface MicrosoftAdvertisingClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used for Retry-After dates. */ now?: () => Date;
}

/** One bounded provider response or classified local failure. */
export interface MicrosoftAdvertisingExchange {
  /** HTTP status, or zero before response headers. */ status: number;
  /** Response bytes retained only during this call. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local failure class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Microsoft Advertising client resolving renewable OAuth credentials for each call. */
export class MicrosoftAdvertisingClient {
  private readonly baseURL: string;
  private readonly customerID: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate connection settings without contacting Microsoft Advertising. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MicrosoftAdvertisingClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Microsoft Advertising configuration and credentials are required');
    if (!validAdvertisingID(config.customerId)) throw new TypeError('Microsoft Advertising customer ID is invalid');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://campaign.api.bingads.microsoft.com');
    this.customerID = config.customerId;
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Microsoft Advertising request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Microsoft Advertising', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the campaign discovery Query. */ listCampaigns(): ListCampaignsOperation { return new ListCampaignsOperation(this); }

  /** Send one bounded campaign query with credentials outside Flow state. */
  async postCampaigns(call: Call, accountID: string): Promise<MicrosoftAdvertisingExchange> {
    let accessToken: string;
    let developerToken: string;
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      accessToken = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      developerToken = resolved.developerToken instanceof SecretString ? resolved.developerToken.reveal() : '';
      if (!isHeaderSafeCredential(accessToken) || !isHeaderSafeCredential(developerToken)) return failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    const request: ProviderRequest = {
      url: this.baseURL + campaignsPath,
      method: 'POST',
      headers: {
        Accept: 'application/json', 'Content-Type': 'application/json',
        Authorization: `Bearer ${accessToken}`, DeveloperToken: developerToken,
        CustomerId: this.customerID, CustomerAccountId: accountID,
      },
      body: Buffer.from(JSON.stringify({ AccountId: accountID, CampaignType: allCampaignTypes })),
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()) };
      return failedExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(accessToken) || Buffer.from(response.body).includes(developerToken))
      return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

function failedExchange(error: NonNullable<MicrosoftAdvertisingExchange['error']>): MicrosoftAdvertisingExchange {
  return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error };
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
