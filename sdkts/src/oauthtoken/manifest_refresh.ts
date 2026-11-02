// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError } from '../errors.js';
import { SecretString, type CredentialRefreshDriver, type CredentialRefreshResult, type CredentialRefreshState } from '../credentials.js';
import {
  hasAllScopes, isRefreshRequired, parseRSAPrivateKeyPEM, signJWTBearerAssertion, TokenEndpoint,
  type ClientAuthenticationMethod, type TokenResponse,
} from './index.js';

type ManifestObject = Record<string, unknown>;
type GrantKind = 'refresh' | 'client_credentials' | 'jwt_bearer';

interface Grant {
  kind: GrantKind;
  endpoint: unknown;
  authentication: unknown;
  encoding: string;
  clientID: string;
  clientSecret: string;
  refreshField: string;
  keyCredential: string;
  assertion: ManifestObject;
  mappings: ManifestObject[];
  copies: ManifestObject[];
  scopes: unknown;
  requestScopes: unknown;
  terminalCodes: string[];
  acceptedTokenTypes: string[];
  acceptsMissingTokenType: boolean;
  missingExpiry: string;
  additional: Record<string, string>;
}

/** Manifest-backed OAuth and service-account grant options emitted by connectorctl. */
export interface ManifestCredentialRefreshOptions {
  /** Safe provider name used in token endpoint errors. */ providerName: string;
  /** Validated spec.auth declaration, encoded using the manifest JSON contract. */ auth: ManifestObject;
  /** Credential wire field name to connector runtime property. */ credentialFields: Readonly<Record<string, string>>;
  /** Startup-pinned connection settings that value sources may read. */ settings?: Readonly<Record<string, string>>;
  /** Caller-owned token HTTP transport. */ fetch?: typeof fetch;
  /** Clock for deterministic tests. */ now?: () => Date;
}

/** Exchange manifest-declared grants while a credential provider owns admission and storage.
 *
 * Missing expiry keeps an existing access token. Refresh sends each active grant
 * and reports the earliest expiry. Declared sub-500 terminal errors require
 * reauthorization; other token endpoint failures remain retryable. A credential
 * with multiple methods uses authMethodId or the manifest default method.
 */
export class ManifestCredentialRefreshDriver<C extends object> implements CredentialRefreshDriver<C> {
  readonly #providerName: string;
  readonly #auth: ManifestObject;
  readonly #credentialFields: Readonly<Record<string, string>>;
  readonly #settings: Readonly<Record<string, string>>;
  readonly #fetch: typeof fetch;
  readonly #now: () => Date;

  /** Bind validated auth data, pinned settings, a caller-owned transport, and a clock. */
  constructor(options: ManifestCredentialRefreshOptions) {
    if (!options?.providerName || !options.auth || !options.credentialFields) throw new TypeError('manifest refresh configuration is required');
    this.#providerName = options.providerName;
    this.#auth = options.auth;
    this.#credentialFields = options.credentialFields;
    this.#settings = options.settings ?? {};
    this.#fetch = options.fetch ?? fetch;
    this.#now = options.now ?? (() => new Date());
  }

  /** Check declared access fields against the host's single credential expiry. */
  refreshRequired(state: CredentialRefreshState<C>): boolean {
    const grants = this.#grants(this.#selectedMethod(state.credentials));
    if (!grants.length) return false;
    const accessFields = grants.flatMap(grant => grant.mappings.filter(mapping => mapping.source === 'access_token').map(mapping => text(mapping.credential)));
    const hasAccessToken = accessFields.length > 0 && accessFields.every(name => Boolean(this.#credentialValue(state.credentials, name)));
    return isRefreshRequired(hasAccessToken, state.expiresAt, state.now, 'keep');
  }

  /** Send each active grant and return one complete credential with the earliest expiry. */
  async refresh(state: CredentialRefreshState<C>, signal: AbortSignal): Promise<CredentialRefreshResult<C>> {
    let credentials = state.credentials;
    const method = this.#selectedMethod(credentials);
    const grants = this.#grants(method);
    if (!grants.length) throw new ReauthorizationRequiredError();
    const expiries: number[] = [];
    for (const grant of grants) {
      if (grant.refreshField && !this.#credentialValue(credentials, grant.refreshField)) continue;
      const token = await this.#exchange(grant, credentials, method, signal);
      const requiredScopes = this.#scopes(grant.scopes).filter(scope => grant.kind !== 'refresh' || scope !== 'offline_access');
      if (token.scope && !hasAllScopes(token.scope, requiredScopes)) throw new ReauthorizationRequiredError();
      const updates: Record<string, unknown> = {};
      for (const mapping of grant.mappings) {
        const destination = text(mapping.credential);
        const value = token.mappedCredentialValue(text(mapping.source));
        if (value === undefined || (value === '' && destination === grant.refreshField)) continue;
        const property = this.#credentialFields[destination];
        if (!property) throw new TypeError('manifest credential field is not mapped');
        updates[property] = this.#isSecret(method, destination) ? new SecretString(value) : value;
      }
      for (const copy of grant.copies) {
        const destination = text(copy.credential);
        const property = this.#credentialFields[destination];
        if (!property) throw new TypeError('manifest credential field is not mapped');
        const value = this.#credentialValue(credentials, text(copy.fromCredential));
        updates[property] = this.#isSecret(method, destination) ? new SecretString(value) : value;
      }
      credentials = { ...credentials, ...updates };
      const lifetime = token.expiresInMs > 0 ? token.expiresInMs : parseDurationMs(grant.missingExpiry);
      expiries.push(lifetime > 0 ? this.#now().getTime() + lifetime : 0);
    }
    if (!expiries.length) throw new ReauthorizationRequiredError();
    return { credentials, expiresAt: new Date(Math.min(...expiries)) };
  }

  #selectedMethod(credentials: C): ManifestObject {
    const methods = list(this.#auth.methods);
    if (!methods.length) return this.#auth;
    const selected = text((credentials as Record<string, unknown>).authMethodId) || text(this.#auth.defaultMethod);
    const method = methods.map(object).find(item => item.id === selected);
    if (!method) throw new ReauthorizationRequiredError();
    return method;
  }

  #grants(method: ManifestObject): Grant[] {
    if (method.oauth2) {
      const oauth = object(method.oauth2);
      const refresh = method.oauth2 && object(oauth.refresh);
      let declared = list(refresh?.grants).map(object);
      if (!declared.length) {
        const mapped = list(oauth.credentialMappings).map(object).find(mapping => mapping.source === 'refresh_token');
        if (!mapped) return [];
        declared = [{ refreshTokenCredential: mapped.credential, credentialMappings: oauth.credentialMappings }];
      }
      return declared.map(declaration => ({
        kind: 'refresh', endpoint: oauth.tokenEndpoint, authentication: oauth.tokenEndpointAuthMethod ?? 'client_secret_post',
        encoding: text(oauth.tokenRequestEncoding) || 'form', clientID: text(oauth.clientIDCredential),
        clientSecret: text(oauth.clientSecretCredential), refreshField: text(declaration.refreshTokenCredential),
        keyCredential: '', assertion: {}, mappings: list(declaration.credentialMappings).map(object), copies: [],
        scopes: declaration.grantedScopes === 'userScopes' ? oauth.userScopes : oauth.scopes, requestScopes: [],
        terminalCodes: strings(refresh?.terminalErrorCodes, ['invalid_grant']),
        acceptedTokenTypes: strings(declaration.acceptedTokenTypes ?? refresh?.acceptedTokenTypes, ['Bearer']),
        acceptsMissingTokenType: refresh?.acceptsMissingTokenType === true,
        missingExpiry: missingExpiryLifetime(refresh?.missingExpiry), additional: stringRecord(refresh?.additionalParameters),
      }));
    }
    if (method.clientCredentials) {
      const client = object(method.clientCredentials);
      return [{
        kind: 'client_credentials', endpoint: client.tokenEndpoint,
        authentication: client.tokenEndpointAuthMethod ?? 'client_secret_post',
        encoding: text(client.tokenRequestEncoding) || 'form', clientID: text(client.clientIDCredential),
        clientSecret: text(client.clientSecretCredential), refreshField: '', keyCredential: '', assertion: {},
        mappings: list(client.credentialMappings).map(object), copies: [], scopes: client.scopes,
        requestScopes: client.scopes, terminalCodes: strings(client.terminalErrorCodes, ['invalid_client']),
        acceptedTokenTypes: strings(client.acceptedTokenTypes, ['Bearer']),
        acceptsMissingTokenType: client.acceptsMissingTokenType === true,
        missingExpiry: missingExpiryLifetime(client.missingExpiry), additional: stringRecord(client.additionalParameters),
      }];
    }
    const serviceAccount = object(method.serviceAccount);
    if (!serviceAccount.jwtBearer) return [];
    const bearer = object(serviceAccount.jwtBearer);
    return [{
      kind: 'jwt_bearer', endpoint: bearer.tokenEndpoint, authentication: '', encoding: 'form',
      clientID: '', clientSecret: '', refreshField: '', keyCredential: text(bearer.keyCredential),
      assertion: bearer, mappings: list(bearer.credentialMappings).map(object),
      copies: list(bearer.credentialCopies).map(object), scopes: bearer.scopes, requestScopes: [],
      terminalCodes: strings(bearer.terminalErrorCodes, ['invalid_grant']),
      acceptedTokenTypes: strings(bearer.acceptedTokenTypes, ['Bearer']),
      acceptsMissingTokenType: bearer.acceptsMissingTokenType === true,
      missingExpiry: missingExpiryLifetime(bearer.missingExpiry), additional: {},
    }];
  }

  async #exchange(grant: Grant, credentials: C, method: ManifestObject, signal: AbortSignal): Promise<TokenResponse> {
    let key: ManifestObject = {};
    if (grant.kind === 'jwt_bearer') {
      try { key = object(JSON.parse(this.#credentialValue(credentials, grant.keyCredential)) as unknown); }
      catch { throw new ReauthorizationRequiredError(); }
    }
    const endpoint = new TokenEndpoint({
      providerName: this.#providerName, url: this.#resolveSource(grant.endpoint, credentials, key),
      terminalErrorCodes: grant.terminalCodes, acceptedTokenTypes: grant.acceptedTokenTypes,
      acceptsMissingTokenType: grant.acceptsMissingTokenType,
      acceptsMissingExpiresIn: true, usesJSONRequestBody: grant.encoding === 'json',
      additionalRequestParameters: grant.additional,
      credentialMappingPaths: grant.mappings.map(mapping => text(mapping.source)), fetch: this.#fetch,
    });
    if (grant.kind === 'jwt_bearer') {
      try {
        const assertion = grant.assertion;
        const signed = signJWTBearerAssertion({
          issuer: this.#resolveSource(assertion.issuer, credentials, key),
          audience: this.#resolveSource(assertion.audience, credentials, key),
          ...(assertion.subject ? { subject: this.#resolveSource(assertion.subject, credentials, key) } : {}),
          scope: this.#scopes(assertion.scopes).join(' '), issuedAt: this.#now(),
          lifetimeMs: parseDurationMs(text(assertion.lifetime)),
        }, parseRSAPrivateKeyPEM(this.#resolveSource(assertion.privateKey, credentials, key)));
        return endpoint.exchangeJWTBearerAssertion(signed, signal);
      } catch { throw new ReauthorizationRequiredError(); }
    }
    const secret = this.#credentialValue(credentials, grant.clientSecret);
    if (!secret && this.#isRequired(method, grant.clientSecret)) throw new ReauthorizationRequiredError();
    const authenticationMethod = secret || grant.kind === 'client_credentials'
      ? this.#resolveSource(grant.authentication, credentials, key) as ClientAuthenticationMethod : 'none';
    const client = { id: this.#credentialValue(credentials, grant.clientID), secret: new SecretString(secret), authenticationMethod };
    if (grant.kind === 'refresh') {
      return endpoint.exchangeRefreshToken(client, new SecretString(this.#credentialValue(credentials, grant.refreshField)), signal);
    }
    return endpoint.exchangeClientCredentials(client, this.#scopes(grant.requestScopes), signal);
  }

  #credentialValue(credentials: C, name: string): string {
    const property = this.#credentialFields[name];
    if (!property) return '';
    const value: unknown = (credentials as Record<string, unknown>)[property];
    return value instanceof SecretString ? value.reveal() : typeof value === 'string' ? value : '';
  }

  #isSecret(method: ManifestObject, name: string): boolean {
    return [...list(this.#auth.fields), ...list(method.fields)].map(object)
      .some(field => field.name === name && field.type === 'secretString');
  }

  #isRequired(method: ManifestObject, name: string): boolean {
    return [...list(this.#auth.fields), ...list(method.fields)].map(object)
      .some(field => field.name === name && field.required === true);
  }

  #scopes(source: unknown): string[] {
    if (Array.isArray(source)) return source.filter((value): value is string => typeof value === 'string');
    const fromSetting = object(source).fromSetting;
    return typeof fromSetting === 'string' ? (this.#settings[fromSetting] ?? '').split(/\s+/).filter(Boolean) : [];
  }

  #resolveSource(source: unknown, credentials: C, key: ManifestObject): string {
    if (typeof source === 'string') return source;
    const valueSource = object(source);
    if (typeof valueSource.value === 'string') return valueSource.value;
    if (typeof valueSource.template === 'string') return valueSource.template.replace(/\{([^{}]+)\}/g, (_, name: string) =>
      pathEscape(this.#credentialFields[name] ? this.#credentialValue(credentials, name) : this.#settings[name] ?? ''));
    let value = '';
    if (typeof valueSource.fromCredential === 'string') value = this.#credentialValue(credentials, valueSource.fromCredential);
    else if (typeof valueSource.fromSetting === 'string') value = this.#settings[valueSource.fromSetting] ?? '';
    else if (typeof valueSource.fromKey === 'string') value = text(key[valueSource.fromKey]);
    else throw new ReauthorizationRequiredError();
    if (valueSource.values) {
      const translated = object(valueSource.values)[value];
      if (typeof translated !== 'string') throw new ReauthorizationRequiredError();
      return translated;
    }
    if (!value) throw new ReauthorizationRequiredError();
    return value;
  }
}

function object(value: unknown): ManifestObject {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as ManifestObject : {};
}
function list(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function text(value: unknown): string { return typeof value === 'string' ? value : ''; }
function strings(value: unknown, fallback: string[]): string[] {
  const found = list(value).filter((item): item is string => typeof item === 'string');
  return found.length ? found : fallback;
}
function stringRecord(value: unknown): Record<string, string> {
  return Object.fromEntries(Object.entries(object(value)).filter((entry): entry is [string, string] => typeof entry[1] === 'string'));
}
function missingExpiryLifetime(value: unknown): string { return text(object(value).lifetime); }
function pathEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%(?:2B|26|3D|3A|40|24)/g, escaped => ({ '%2B': '+', '%26': '&', '%3D': '=', '%3A': ':', '%40': '@', '%24': '$' })[escaped] ?? escaped);
}
function parseDurationMs(value: string): number {
  if (!value) return 0;
  const parts = value.matchAll(/(\d+(?:\.\d+)?)(h|m|s)/g);
  let duration = 0; let position = 0;
  for (const part of parts) {
    if (part.index !== position) throw new TypeError('manifest grant duration is invalid');
    duration += Number(part[1]) * ({ h: 3_600_000, m: 60_000, s: 1000 }[part[2] as 'h' | 'm' | 's']);
    position += part[0].length;
  }
  if (position !== value.length || duration <= 0 || !Number.isSafeInteger(duration)) throw new TypeError('manifest grant duration is invalid');
  return duration;
}
