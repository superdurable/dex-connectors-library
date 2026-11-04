// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError } from '../errors.js';
import { isIP } from 'node:net';
import { SecretString, type CredentialRefreshDriver, type CredentialRefreshResult, type CredentialRefreshState } from '../credentials.js';
import {
  isRefreshRequired, parseRSAPrivateKeyPEM, signJWTBearerAssertion, TokenEndpoint,
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
  acceptedScopeSets: unknown;
  requestScopes: unknown;
  terminalCodes: string[];
  acceptedTokenTypes: string[];
  exactScopes: boolean;
  scopeSeparators: 'whitespace' | 'whitespaceAndComma';
  allowsPublicClient: boolean;
  acceptsMissingTokenType: boolean;
  acceptsMissingExpiresIn: boolean;
  storedMissingExpiry: 'refresh' | 'keep';
  missingExpiry: string;
  additionalParameters: Record<string, string>;
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
 * Stored missing expiry follows storedMissingExpiry, defaulting to keep. Refresh sends each active grant
 * and reports the earliest expiry. Declared sub-500 terminal errors require
 * reauthorization; other token endpoint failures remain retryable. A credential
 * with multiple methods uses the declared default when authMethodId is empty.
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
    const accessFields = grants.flatMap(grant => grant.mappings.filter(mapping => mapping.source === 'access_token').map(mapping => manifestText(mapping.credential)));
    const hasAccessToken = accessFields.length > 0 && accessFields.every(name => Boolean(this.#credentialValue(state.credentials, name)));
    return isRefreshRequired(hasAccessToken, state.expiresAt, state.now,
      grants.some(grant => grant.storedMissingExpiry === 'refresh') ? 'refresh' : 'keep');
  }

  /** Send each active grant and return one complete credential with the earliest expiry. */
  async refresh(state: CredentialRefreshState<C>, signal: AbortSignal): Promise<CredentialRefreshResult<C>> {
    let credentials = state.credentials;
    const method = this.#selectedMethod(credentials);
    const grants = this.#grants(method);
    if (!grants.length) throw new ReauthorizationRequiredError();
    for (const grant of grants) this.#validateGrant(grant);
    const expiries: number[] = [];
    for (const grant of grants) {
      if (grant.refreshField && !this.#credentialValue(credentials, grant.refreshField)) continue;
      const token = await this.#exchange(grant, credentials, method, signal);
      const requiredScopes = this.#scopes(grant.scopes).filter(scope => grant.kind !== 'refresh' || scope !== 'offline_access');
      const acceptedScopeSets = manifestList(grant.acceptedScopeSets).map(manifestList)
        .map(set => set.filter((scope): scope is string => typeof scope === 'string'));
      const scopeSets = acceptedScopeSets.length ? acceptedScopeSets : [requiredScopes];
      if (grant.exactScopes ? !scopeSets.some(set => hasDeclaredScopes(token.scope, set, grant.scopeSeparators, true))
        : token.scope.trim() !== '' && !scopeSets.some(set => hasDeclaredScopes(token.scope, set, grant.scopeSeparators, false))) throw new ReauthorizationRequiredError();
      const updates: Record<string, unknown> = {};
      for (const mapping of grant.mappings) {
        const destination = manifestText(mapping.credential);
        const value = token.mappedCredentialValue(manifestText(mapping.source));
        if (value === undefined || (value === '' && destination === grant.refreshField)) continue;
        const property = this.#credentialFields[destination];
        if (!property) throw new TypeError('manifest credential field is not mapped');
        updates[property] = this.#isSecret(method, destination) ? new SecretString(value) : value;
      }
      for (const copy of grant.copies) {
        const destination = manifestText(copy.credential);
        const property = this.#credentialFields[destination];
        if (!property) throw new TypeError('manifest credential field is not mapped');
        const value = this.#credentialValue(credentials, manifestText(copy.fromCredential));
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
    const methods = manifestList(this.#auth.methods);
    if (!methods.length) return this.#auth;
    const selected = manifestText((credentials as Record<string, unknown>).authMethodId);
    const method = methods.map(manifestObject).find(item => item.id === (selected || manifestText(this.#auth.defaultMethod)));
    if (!method) throw new ReauthorizationRequiredError();
    return method;
  }

  #grants(method: ManifestObject): Grant[] {
    if (method.oauth2) {
      const oauth = manifestObject(method.oauth2);
      const refresh = method.oauth2 && manifestObject(oauth.refresh);
      let declared = manifestList(refresh?.grants).map(manifestObject);
      if (!declared.length) {
        const mapped = manifestList(oauth.credentialMappings).map(manifestObject).find(mapping => mapping.source === 'refresh_token');
        if (!mapped) return [];
        declared = [{ refreshTokenCredential: mapped.credential, credentialMappings: oauth.credentialMappings }];
      }
      return declared.map(declaration => ({
        kind: 'refresh', endpoint: oauth.tokenEndpoint, authentication: oauth.tokenEndpointAuthMethod ?? 'client_secret_post',
        encoding: manifestText(oauth.tokenRequestEncoding) || 'form', clientID: manifestText(oauth.clientIDCredential),
        clientSecret: manifestText(oauth.clientSecretCredential), refreshField: manifestText(declaration.refreshTokenCredential),
        keyCredential: '', assertion: {}, mappings: manifestList(declaration.credentialMappings).map(manifestObject), copies: [],
        scopes: declaration.grantedScopes === 'userScopes' ? oauth.userScopes : (refresh?.requiredScopes ?? oauth.scopes), requestScopes: [],
        acceptedScopeSets: refresh?.acceptedScopeSets,
        terminalCodes: strings(refresh?.terminalErrorCodes, ['invalid_grant']),
        acceptedTokenTypes: strings(declaration.acceptedTokenTypes ?? refresh?.acceptedTokenTypes, ['Bearer']),
        exactScopes: declaration.exactScopes === true || (declaration.exactScopes === undefined && refresh?.exactScopes === true),
        scopeSeparators: refresh?.scopeSeparators === 'whitespaceAndComma' ? 'whitespaceAndComma' : 'whitespace',
        allowsPublicClient: refresh?.allowsPublicClient === true,
        acceptsMissingTokenType: declaration.acceptsMissingTokenType === undefined
          ? refresh?.acceptsMissingTokenType === true : declaration.acceptsMissingTokenType === true,
        acceptsMissingExpiresIn: refresh?.missingExpiry !== undefined,
        storedMissingExpiry: refresh?.storedMissingExpiry === 'refresh' ? 'refresh' : 'keep',
        missingExpiry: missingExpiryLifetime(refresh?.missingExpiry), additionalParameters: stringRecord(refresh?.additionalParameters),
      }));
    }
    if (method.clientCredentials) {
      const client = manifestObject(method.clientCredentials);
      return [{
        kind: 'client_credentials', endpoint: client.tokenEndpoint,
        authentication: client.tokenEndpointAuthMethod ?? 'client_secret_post',
        encoding: manifestText(client.tokenRequestEncoding) || 'form', clientID: manifestText(client.clientIDCredential),
        clientSecret: manifestText(client.clientSecretCredential), refreshField: '', keyCredential: '', assertion: {},
        mappings: manifestList(client.credentialMappings).map(manifestObject), copies: [], scopes: client.scopes,
        requestScopes: client.scopes, acceptedScopeSets: [], terminalCodes: strings(client.terminalErrorCodes, ['invalid_client']),
        acceptedTokenTypes: strings(client.acceptedTokenTypes, ['Bearer']),
        exactScopes: false,
        scopeSeparators: 'whitespace', allowsPublicClient: false,
        acceptsMissingTokenType: client.acceptsMissingTokenType === true,
        acceptsMissingExpiresIn: client.missingExpiry !== undefined,
        storedMissingExpiry: client.storedMissingExpiry === 'refresh' ? 'refresh' : 'keep',
        missingExpiry: missingExpiryLifetime(client.missingExpiry), additionalParameters: stringRecord(client.additionalParameters),
      }];
    }
    const serviceAccount = manifestObject(method.serviceAccount);
    if (!serviceAccount.jwtBearer) return [];
    const bearer = manifestObject(serviceAccount.jwtBearer);
    return [{
      kind: 'jwt_bearer', endpoint: bearer.tokenEndpoint, authentication: '', encoding: 'form',
      clientID: '', clientSecret: '', refreshField: '', keyCredential: manifestText(bearer.keyCredential),
      assertion: bearer, mappings: manifestList(bearer.credentialMappings).map(manifestObject),
      copies: manifestList(bearer.credentialCopies).map(manifestObject), scopes: bearer.scopes, requestScopes: [], acceptedScopeSets: [],
      terminalCodes: strings(bearer.terminalErrorCodes, ['invalid_grant']),
      acceptedTokenTypes: strings(bearer.acceptedTokenTypes, ['Bearer']),
      exactScopes: false,
      scopeSeparators: 'whitespace', allowsPublicClient: false,
      acceptsMissingTokenType: bearer.acceptsMissingTokenType === true,
      acceptsMissingExpiresIn: bearer.missingExpiry !== undefined,
      storedMissingExpiry: bearer.storedMissingExpiry === 'refresh' ? 'refresh' : 'keep',
      missingExpiry: missingExpiryLifetime(bearer.missingExpiry), additionalParameters: {},
    }];
  }

  async #exchange(grant: Grant, credentials: C, method: ManifestObject, signal: AbortSignal): Promise<TokenResponse> {
    let key: ManifestObject = {};
    if (grant.kind === 'jwt_bearer') {
      try { key = manifestObject(JSON.parse(this.#credentialValue(credentials, grant.keyCredential)) as unknown); }
      catch { throw new ReauthorizationRequiredError(); }
    }
    const endpoint = new TokenEndpoint({
      providerName: this.#providerName, url: this.#resolveSource(grant.endpoint, credentials, key),
      terminalErrorCodes: grant.terminalCodes, acceptedTokenTypes: grant.acceptedTokenTypes,
      acceptsMissingTokenType: grant.acceptsMissingTokenType,
      acceptsMissingExpiresIn: grant.acceptsMissingExpiresIn, usesJSONRequestBody: grant.encoding === 'json',
      additionalRequestParameters: grant.additionalParameters,
      credentialMappingPaths: grant.mappings.map(mapping => manifestText(mapping.source)), fetch: this.#fetch,
    });
    if (grant.kind === 'jwt_bearer') {
      const assertion = grant.assertion;
      const lifetimeMs = parseDurationMs(manifestText(assertion.lifetime));
      let signed: SecretString;
      try {
        const subject = assertion.subject ? this.#resolveSource(assertion.subject, credentials, key) : '';
        const validatedSubject = assertion.subjectValidation === 'goMailAddress' ? trimGoSpace(subject) : subject;
        if (assertion.subjectValidation === 'goMailAddress' && !isGoMailAddress(validatedSubject))
          throw new ReauthorizationRequiredError();
        signed = signJWTBearerAssertion({
          issuer: this.#resolveSource(assertion.issuer, credentials, key),
          audience: this.#resolveSource(assertion.audience, credentials, key),
          ...(assertion.subject ? { subject: validatedSubject } : {}),
          scope: this.#scopes(assertion.scopes).join(' '), issuedAt: this.#now(),
          lifetimeMs,
        }, parseRSAPrivateKeyPEM(this.#resolveSource(assertion.privateKey, credentials, key)));
      } catch { throw new ReauthorizationRequiredError(); }
      return await endpoint.exchangeJWTBearerAssertion(signed, signal);
    }
    const secret = this.#credentialValue(credentials, grant.clientSecret);
    if (!secret && !grant.allowsPublicClient && (grant.kind === 'refresh' || this.#isRequired(method, grant.clientSecret)))
      throw new ReauthorizationRequiredError();
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
    return manifestList(method.fields).map(manifestObject)
      .some(field => field.name === name && field.type === 'secretString');
  }

  #isRequired(method: ManifestObject, name: string): boolean {
    const field = manifestList(method.fields).map(manifestObject)
      .find(candidate => candidate.name === name);
    return field ? field.required === true : true;
  }

  #validateGrant(grant: Grant): void {
    for (const mapping of grant.mappings) {
      if (!manifestText(mapping.source) || !this.#credentialFields[manifestText(mapping.credential)])
        throw new TypeError('manifest credential field is not mapped');
    }
    for (const copy of grant.copies) {
      if (!this.#credentialFields[manifestText(copy.credential)] || !this.#credentialFields[manifestText(copy.fromCredential)])
        throw new TypeError('manifest credential field is not mapped');
    }
    if (grant.missingExpiry && parseDurationMs(grant.missingExpiry) <= 0)
      throw new TypeError('manifest grant duration must be positive');
    if (grant.kind === 'jwt_bearer' && parseDurationMs(manifestText(grant.assertion.lifetime)) < 1)
      throw new TypeError('manifest JWT assertion lifetime must be at least one millisecond');
  }

  #scopes(source: unknown): string[] {
    if (Array.isArray(source)) return source.filter((value): value is string => typeof value === 'string');
    const fromSetting = manifestObject(source).fromSetting;
    return typeof fromSetting === 'string' ? (this.#settings[fromSetting] ?? '').split(/\s+/).filter(Boolean) : [];
  }

  #resolveSource(source: unknown, credentials: C, key: ManifestObject): string {
    if (typeof source === 'string') return source;
    const valueSource = manifestObject(source);
    if (typeof valueSource.value === 'string') return valueSource.value;
    if (typeof valueSource.template === 'string') return valueSource.template.replace(/\{([^{}]+)\}/g, (_, name: string) =>
      pathEscape(this.#credentialFields[name] ? this.#credentialValue(credentials, name) : this.#settings[name] ?? ''));
    let value = '';
    if (typeof valueSource.fromCredential === 'string') value = this.#credentialValue(credentials, valueSource.fromCredential);
    else if (typeof valueSource.fromSetting === 'string') value = this.#settings[valueSource.fromSetting] ?? '';
    else if (typeof valueSource.fromKey === 'string') value = manifestText(key[valueSource.fromKey]);
    else throw new ReauthorizationRequiredError();
    if (valueSource.values) {
      const translated = manifestObject(valueSource.values)[value];
      if (typeof translated !== 'string') throw new ReauthorizationRequiredError();
      return translated;
    }
    if (!value) throw new ReauthorizationRequiredError();
    return value;
  }
}

function manifestObject(value: unknown): ManifestObject {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as ManifestObject : {};
}
function manifestList(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function manifestText(value: unknown): string { return typeof value === 'string' ? value : ''; }
function strings(value: unknown, fallback: string[]): string[] {
  const found = manifestList(value).filter((item): item is string => typeof item === 'string');
  return found.length ? found : fallback;
}
function stringRecord(value: unknown): Record<string, string> {
  return Object.fromEntries(Object.entries(manifestObject(value)).filter((entry): entry is [string, string] => typeof entry[1] === 'string'));
}
function missingExpiryLifetime(value: unknown): string { return manifestText(manifestObject(value).lifetime); }
function hasDeclaredScopes(returnedScopes: string, requiredScopes: readonly string[], separators: Grant['scopeSeparators'], exact: boolean): boolean {
  const granted = new Set(returnedScopes.split(separators === 'whitespaceAndComma' ? /[ ,]+/ : /\s+/).filter(Boolean));
  const required = new Set(requiredScopes);
  return (!exact || granted.size === required.size) && [...required].every(scope => granted.has(scope));
}

function trimGoSpace(value: string): string {
  const isSpace = (code: number): boolean => code === 0x20 || (code >= 0x09 && code <= 0x0d)
    || code === 0x85 || code === 0xa0 || code === 0x1680 || (code >= 0x2000 && code <= 0x200a)
    || code === 0x2028 || code === 0x2029 || code === 0x202f || code === 0x205f || code === 0x3000;
  const characters = Array.from(value);
  while (characters.length && isSpace(characters[0]!.codePointAt(0)!)) characters.shift();
  while (characters.length && isSpace(characters.at(-1)!.codePointAt(0)!)) characters.pop();
  return characters.join('');
}

// Address equality after Go mail.ParseAddress leaves only its addr-spec grammar.
function isGoMailAddress(value: string): boolean {
  const at = value.indexOf('@');
  if (at <= 0 || at !== value.lastIndexOf('@') || at === value.length - 1) return false;
  const local = value.slice(0, at);
  const domain = value.slice(at + 1);
  const isAtom = (text: string): boolean => {
    if (!text || text.startsWith('.') || text.endsWith('.') || text.includes('..')) return false;
    for (const character of text) {
      const code = character.codePointAt(0)!;
      if (code >= 0xd800 && code <= 0xdfff) return false;
      if (code >= 0x80) continue;
      if (code < 0x21 || code > 0x7e || '()<>[]:;@\\,"'.includes(character)) return false;
    }
    return true;
  };
  if (!isAtom(local)) return false;
  if (domain.startsWith('[') && domain.endsWith(']')) return isIP(domain.slice(1, -1)) !== 0;
  return isAtom(domain);
}
function pathEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%(?:2B|26|3D|3A|40|24)/g, escaped => ({ '%2B': '+', '%26': '&', '%3D': '=', '%3A': ':', '%40': '@', '%24': '$' })[escaped] ?? escaped);
}
function parseDurationMs(value: string): number {
  if (!value) return 0;
  if (value === '0' || value === '+0' || value === '-0') return 0;
  const sign = value[0] === '-' ? -1n : 1n;
  const magnitude = value[0] === '+' || value[0] === '-' ? value.slice(1) : value;
  const parts = magnitude.matchAll(/((?:\d+(?:\.\d*)?|\.\d+))(ns|us|µs|μs|ms|s|m|h)/g);
  const nanoseconds: Record<string, bigint> = {
    ns: 1n, us: 1_000n, 'µs': 1_000n, 'μs': 1_000n,
    ms: 1_000_000n, s: 1_000_000_000n, m: 60_000_000_000n, h: 3_600_000_000_000n,
  };
  let duration = 0n; let position = 0;
  for (const part of parts) {
    if (part.index !== position) throw new TypeError('manifest grant duration is invalid');
    const [whole, fraction = ''] = part[1]!.split('.');
    const unit = nanoseconds[part[2]!]!;
    duration += BigInt(whole || '0') * unit;
    if (fraction) duration += BigInt(fraction) * unit / 10n ** BigInt(fraction.length);
    position += part[0].length;
  }
  if (position !== magnitude.length || duration === 0n
    || duration > (sign < 0n ? 9_223_372_036_854_775_808n : 9_223_372_036_854_775_807n))
    throw new TypeError('manifest grant duration is invalid');
  return Number(sign * duration) / 1_000_000;
}
