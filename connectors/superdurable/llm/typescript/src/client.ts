// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { lookup } from 'node:dns/promises';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { AddressRefusal, sendProviderRequest, validateBaseURL, type ProviderHTTPClient } from '@superdurable/connectors-sdk/providerhttp';
import { newTextGenerationQuery, type TextGenerationQuery } from '@superdurable/connectors-sdk/textgen';
import type { Config, CredentialSource } from './generated/models.js';
import { GenerateTextDefinition } from './generated/definitions.js';
import { newChatWireFormat } from './chat-profiles.js';
import { newResponsesWireFormat } from './responses-wire-format.js';
import { newClaudeWireFormat } from './claude-wire-format.js';
import { newGeminiWireFormat } from './gemini-wire-format.js';
import { isAllowedOpenAICompatibleAddress, isLoopbackAddress } from './address-policy.js';

const providerSettings = {
  openai: { model: 'gpt-6-sol', urls: { global: 'https://api.openai.com/v1' } },
  'openai-compatible': { model: '', urls: { global: '' } },
  anthropic: { model: 'claude-sonnet-5', urls: { global: 'https://api.anthropic.com' } },
  gemini: { model: 'gemini-3.5-flash-lite', urls: { global: 'https://generativelanguage.googleapis.com/v1beta' } },
  qwen: { model: 'qwen3.7-plus', urls: {
    global: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1',
    'hong-kong': 'https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1',
    china: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
  } },
  deepseek: { model: 'deepseek-flash', urls: { global: 'https://api.deepseek.com' } },
  meta: { model: 'muse-spark-1.3', urls: { global: 'https://api.meta.ai' } },
  mistral: { model: 'mistral-large-2512', urls: {
    global: 'https://api.mistral.ai/v1', eu: 'https://api.eu.mistral.ai/v1', us: 'https://api.us.mistral.ai/v1',
  } },
  kimi: { model: 'kimi-k2.6', urls: { global: 'https://api.moonshot.ai/v1', china: 'https://api.moonshot.cn/v1' } },
  xai: { model: 'grok-4.3', urls: { global: 'https://api.x.ai/v1', us: 'https://us.api.x.ai/v1' } },
} as const;
function isLoopbackHost(hostname: string): boolean {
  const address = hostname.replace(/^\[|\]$/g, '');
  return address === 'localhost' || isLoopbackAddress(address);
}

async function checkLoopbackAddress(url: string): Promise<void> {
  const hostname = new URL(url).hostname.replace(/^\[|\]$/g, '');
  if (hostname !== 'localhost' && !isLoopbackHost(hostname)) throw new AddressRefusal('provider address is not loopback');
  const addresses = hostname === 'localhost' ? await lookup(hostname, { all: true }) : [{ address: hostname }];
  if (!addresses.length || addresses.some(candidate => !isLoopbackHost(candidate.address))) {
    throw new AddressRefusal('provider address is not loopback');
  }
}

/** Optional transport validation and loopback test endpoint. */
export interface ClientOptions {
  /** Custom HTTP transports are unsupported because their DNS dial cannot be pinned safely. */
  httpClient?: ProviderHTTPClient;
  /** Override provider address with an exact loopback test server URL. */
  baseURLForTest?: string;
}

/** One connection's stateless text generation query. */
export class Client {
  readonly #generateText: TextGenerationQuery;

  /** Validate provider settings and bind a fresh credential resolver. */
  constructor(config: Config, credentials: CredentialSource, options: ClientOptions = {}) {
    if (!credentials) throw new TypeError('llm credential provider is required');
    const provider = config.provider.trim();
    if (!(provider in providerSettings)) throw new TypeError('llm configuration provider is invalid');
    if (options.httpClient) throw new TypeError('llm requires the built-in HTTP transport');
    const settings = providerSettings[provider as keyof typeof providerSettings];
    const region = config.region || 'global';
    if (!(region in settings.urls)) throw new TypeError('llm provider does not serve the configured region');
    if (config.maxResponseBytes !== undefined && (!Number.isSafeInteger(config.maxResponseBytes) || config.maxResponseBytes < 0)) {
      throw new TypeError('llm maxResponseBytes must be nonnegative');
    }
    const model = (config.model ?? '').trim();
    const baseURLValue = (config.baseUrl ?? '').trim();
    if (provider === 'openai-compatible') {
      if (!model) throw new TypeError('llm openai-compatible model is required');
      if (!baseURLValue) throw new TypeError('llm openai-compatible baseUrl is required');
      const validated = validateBaseURL(baseURLValue);
      if (!new URL(validated).pathname.endsWith('/v1')) throw new TypeError('llm openai-compatible baseUrl must end in /v1');
      const host = new URL(validated).hostname.replace(/^\[|\]$/g, '');
      if (isIP(host) && !isAllowedOpenAICompatibleAddress(host, isLoopbackAddress(host))) {
        throw new TypeError('llm openai-compatible baseUrl host must be public or loopback');
      }
    } else if (config.baseUrl) throw new TypeError('llm baseUrl applies only to openai-compatible');
    const workspace = (config.anthropicWorkspaceId ?? '').trim();
    if (workspace && provider !== 'anthropic') throw new TypeError('llm anthropicWorkspaceId applies only to anthropic');
    if (workspace && !/^wrkspc_[A-Za-z0-9]{1,128}$/.test(workspace)) throw new TypeError('llm anthropicWorkspaceId must be a workspace ID');
    let baseURL = provider === 'openai-compatible'
      ? baseURLValue
      : settings.urls[region as keyof typeof settings.urls];
    if (options.baseURLForTest) {
      const parsed = new URL(validateBaseURL(options.baseURLForTest));
      if (!isLoopbackHost(parsed.hostname)) {
        throw new TypeError('llm test base URL must use a loopback host');
      }
      baseURL = options.baseURLForTest;
    }
    const addressPolicyClient: ProviderHTTPClient = {
      send: async request => {
        const isCustomLoopback = provider === 'openai-compatible' && isLoopbackHost(new URL(baseURL).hostname);
        if (isCustomLoopback || options.baseURLForTest) await checkLoopbackAddress(request.url);
        if (provider === 'openai-compatible' && !isCustomLoopback && !options.baseURLForTest) {
          const parsed = new URL(request.url);
          const hostname = parsed.hostname.replace(/^\[|\]$/g, '');
          let addresses: { address: string }[];
          try { addresses = isIP(hostname) ? [{ address: hostname }] : await lookup(hostname, { all: true, verbatim: true }); }
          catch { throw new AddressRefusal('provider address could not be resolved'); }
          if (!addresses.length || addresses.some(candidate => !isAllowedOpenAICompatibleAddress(candidate.address, false))) {
            throw new AddressRefusal('provider address is not allowed');
          }
          const selected = addresses[0]!;
          parsed.hostname = isIP(selected.address) === 6 ? `[${selected.address}]` : selected.address;
          return sendProviderRequest({ ...request, url: parsed.href,
            headers: { ...request.headers, Host: new URL(request.url).host }, addressPolicy: 'unrestricted' });
        }
        return sendProviderRequest({ ...request, addressPolicy: 'unrestricted' });
      },
    };
    const wireFormat = provider === 'openai' ? newResponsesWireFormat()
      : provider === 'anthropic' ? newClaudeWireFormat(workspace)
      : provider === 'gemini' ? newGeminiWireFormat()
      : newChatWireFormat(provider);
    this.#generateText = newTextGenerationQuery({
      definition: GenerateTextDefinition, wireFormat, baseURL,
      connectionModel: model || settings.model,
      httpClient: addressPolicyClient,
      requestTimeoutMs: provider === 'deepseek' ? 1_170_000 : 870_000,
      resolveCredential: async (call: Call) => {
        const resolved = await credentials.resolve(call);
        return resolved.apiKey instanceof SecretString ? resolved.apiKey : new SecretString(String(resolved.apiKey));
      },
      maxResponseBytes: config.maxResponseBytes || (provider === 'deepseek' ? 64 << 20 : 8 << 20),
      maxStreamEventBytes: 1 << 20,
    });
  }

  /** Return this connection's generateText Query. */
  generateText(): TextGenerationQuery { return this.#generateText; }
}
