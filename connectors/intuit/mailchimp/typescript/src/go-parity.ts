// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';

const atomCharacter = /[\x21-\x7e]/;
const atomSpecials = new Set([..."()<>[]:;@\\,\""]);

/** Match net/mail's bare addr-spec acceptance used by the Go connector. */
export function isGoBareEmailAddress(value: string): boolean {
  const separator = value.indexOf('@');
  if (separator <= 0 || separator !== value.lastIndexOf('@')) return false;
  const local = value.slice(0, separator);
  const domain = value.slice(separator + 1);
  const isAtom = (part: string) => !!part && !part.startsWith('.') && !part.endsWith('.') && !part.includes('..')
    && [...part].every(character => {
      const code = character.codePointAt(0)!;
      return code >= 128 ? !(code >= 0xd800 && code <= 0xdfff) : atomCharacter.test(character) && !atomSpecials.has(character);
    });
  if (!isAtom(local)) return false;
  if (domain.startsWith('[') && domain.endsWith(']')) return isIP(domain.slice(1, -1)) !== 0;
  return isAtom(domain);
}

/** Match strings.ToLower's simple Unicode mapping for the subscriber hash. */
export function goSimpleLower(value: string): string {
  return [...value].map(character => character === '\u0130' ? 'i' : character.toLowerCase()).join('');
}

/** Match strings.TrimSpace's Unicode White_Space set, excluding BOM. */
export function goTrimSpace(value: string): string {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '');
}

/** Parse the forms accepted by time.Parse(time.RFC3339) and format as UTC RFC3339Nano. */
export function goRFC3339UTC(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new Error('invalid timestamp');
  const year = Number(match[1]!);
  const month = Number(match[2]!);
  const day = Number(match[3]!);
  const hour = Number(match[4]!);
  const minute = Number(match[5]!);
  const second = Number(match[6]!);
  const fractionText = match[7];
  const zone = match[8]!;
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]! || hour > 23 || minute > 59 || second > 59) throw new Error('invalid timestamp');
  const offsetHours = zone === 'Z' ? 0 : Number(zone.slice(1, 3));
  const offsetMinutes = zone === 'Z' ? 0 : Number(zone.slice(4));
  if (offsetHours > 24 || offsetMinutes > 60) throw new Error('invalid timestamp');
  const instant = new Date(0);
  instant.setUTCFullYear(year, month - 1, day);
  instant.setUTCHours(hour, minute, second);
  instant.setTime(instant.getTime() - (zone.startsWith('-') ? -1 : 1) * (offsetHours * 60 + offsetMinutes) * 60_000);
  if (Number.isNaN(instant.getTime()) || instant.getUTCFullYear() < 0 || instant.getUTCFullYear() > 9999) throw new Error('invalid timestamp');
  const fraction = fractionText?.slice(0, 9).replace(/0+$/, '');
  return instant.toISOString().slice(0, 19) + (fraction ? `.${fraction}` : '') + 'Z';
}
