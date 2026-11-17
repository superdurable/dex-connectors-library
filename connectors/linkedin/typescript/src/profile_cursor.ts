// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import { parseOrderedJSON } from './go_json.js';
import type { ProfileClaims } from './profile_change.js';

/** Committed Go-compatible profile position. */
export interface ProfileCursor { version: number; subject: string; hash: string; sequence: number; profile: ProfileClaims; }

/** Encode a profile cursor in Go struct field order. */
export function encodeProfileCursor(position: ProfileCursor): string {
  const profile = position.profile;
  const claims = [`"subject":${encodeGoJSONValue(profile.subject)}`];
  for (const field of ['name', 'givenName', 'familyName', 'email'] as const) if (profile[field]) claims.push(`${encodeGoJSONValue(field)}:${encodeGoJSONValue(profile[field])}`);
  claims.push(`"emailVerified":${encodeGoJSONValue(profile.emailVerified)}`);
  for (const field of ['pictureUrl', 'locale'] as const) if (profile[field]) claims.push(`${encodeGoJSONValue(field)}:${encodeGoJSONValue(profile[field])}`);
  return `{"version":${position.version},"subject":${encodeGoJSONValue(position.subject)},"hash":${encodeGoJSONValue(position.hash)},${position.sequence ? `"sequence":${position.sequence},` : ''}"profile":{${claims.join(',')}}}`;
}

/** Decode a saved cursor, preserving Go's field folding and repeated-object merge. */
export function decodeProfileCursor(token: string): ProfileCursor | undefined {
  try {
    const document = parseOrderedJSON(token);
    if (document.kind !== 'object') return undefined;
    const position: ProfileCursor = { version: 0, subject: '', hash: '', sequence: 0, profile: { subject: '', emailVerified: false } };
    for (const [key, value] of document.members) {
      const field = fieldName(key, ['version', 'subject', 'hash', 'sequence', 'profile']);
      if (!field || value.kind === 'null') continue;
      if (field === 'version' || field === 'sequence') {
        if (value.kind !== 'number' || !/^-?(?:0|[1-9][0-9]*)$/.test(value.text) || !Number.isSafeInteger(Number(value.text))) return undefined;
        position[field] = Number(value.text);
      } else if (field === 'subject' || field === 'hash') {
        if (value.kind !== 'string') return undefined;
        position[field] = value.value;
      } else {
        if (value.kind !== 'object') return undefined;
        for (const [claimKey, claimValue] of value.members) {
          const claim = fieldName(claimKey, ['subject', 'name', 'givenName', 'familyName', 'email', 'emailVerified', 'pictureUrl', 'locale']);
          if (!claim || claimValue.kind === 'null') continue;
          if (claim === 'emailVerified') {
            if (claimValue.kind !== 'boolean') return undefined;
            position.profile.emailVerified = claimValue.value;
          } else {
            if (claimValue.kind !== 'string') return undefined;
            position.profile[claim as keyof Omit<ProfileClaims, 'emailVerified'>] = claimValue.value;
          }
        }
      }
    }
    if (position.version !== 1 || !position.subject || position.subject !== position.profile.subject || !position.hash || position.sequence < 0) return undefined;
    return position;
  } catch { return undefined; }
}

function fieldName(key: string, names: readonly string[]): string | undefined {
  return names.find(name => key === name) ?? names.find(name => key.toLowerCase().replaceAll('ſ', 's') === name.toLowerCase());
}
