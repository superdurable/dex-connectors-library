// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Text rules of the Go connector's profileChanged Trigger, reproduced byte for byte so that Go and
// TypeScript replicas compute one profile hash and share one poll cursor document.
import { isIP } from 'node:net';

const encoder = new TextEncoder();

/** Decode bytes as Go's encoding/json does inside strings: each invalid byte becomes one U+FFFD. */
export function goDecodeUTF8(bytes: Uint8Array): string {
  let text = '';
  let index = 0;
  while (index < bytes.length) {
    const [codePoint, size] = decodeRune(bytes, index);
    text += String.fromCodePoint(codePoint);
    index += size;
  }
  return text;
}

function decodeRune(bytes: Uint8Array, index: number): [number, number] {
  const first = bytes[index]!;
  if (first < 0x80) return [first, 1];
  const continuation = (offset: number, low = 0x80, high = 0xbf): number | undefined => {
    const value = bytes[index + offset];
    return value !== undefined && value >= low && value <= high ? value & 0x3f : undefined;
  };
  if (first >= 0xc2 && first <= 0xdf) {
    const second = continuation(1);
    if (second !== undefined) return [((first & 0x1f) << 6) | second, 2];
  } else if (first >= 0xe0 && first <= 0xef) {
    const second = continuation(1, first === 0xe0 ? 0xa0 : 0x80, first === 0xed ? 0x9f : 0xbf), third = continuation(2);
    if (second !== undefined && third !== undefined) return [((first & 0x0f) << 12) | (second << 6) | third, 3];
  } else if (first >= 0xf0 && first <= 0xf4) {
    const second = continuation(1, first === 0xf0 ? 0x90 : 0x80, first === 0xf4 ? 0x8f : 0xbf), third = continuation(2), fourth = continuation(3);
    if (second !== undefined && third !== undefined && fourth !== undefined) return [((first & 0x07) << 18) | (second << 12) | (third << 6) | fourth, 4];
  }
  return [0xfffd, 1];
}

/** Replace lone surrogates with U+FFFD, as Go decodes a JSON \ud800 escape and encodes a string. */
export function wellFormed(text: string): string {
  return text.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD');
}

/** Encode a string or boolean as Go's json.Marshal does, including its HTML and line-separator escapes. */
export function goJSON(value: string | boolean): string {
  const text = typeof value === 'string' ? JSON.stringify(wellFormed(value)) : JSON.stringify(value);
  return text.replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Byte length of the UTF-8 encoding. */
export function utf8Length(text: string): number { return encoder.encode(text).length; }

// Go's unicode.IsSpace: Latin-1 spaces, U+0085, U+00A0, and the Unicode White_Space characters.
const goSpace = '[\\t\\n\\v\\f\\r \\u0085\\u00A0\\u1680\\u2000-\\u200A\\u2028\\u2029\\u202F\\u205F\\u3000]';
const goTrimPattern = new RegExp(`^${goSpace}+|${goSpace}+$`, 'gu');

/** Go's strings.TrimSpace, which unlike String.prototype.trim keeps U+FEFF and removes U+0085. */
export function goTrimSpace(text: string): string { return text.replace(goTrimPattern, ''); }

/** Go's boundedString: trim, then keep the longest prefix of whole characters within maximum bytes. */
export function goBoundedString(value: string, maximum: number): string {
  const text = goTrimSpace(value);
  if (utf8Length(text) <= maximum) return text;
  let bounded = '', length = 0;
  for (const character of text) {
    const size = utf8Length(character);
    if (length + size > maximum) break;
    bounded += character;
    length += size;
  }
  return bounded;
}

/** Go's strings.ToLower: each character's simple lower-case mapping, so U+0130 becomes a plain i. */
export function goToLower(text: string): string {
  let lowered = '';
  for (const character of text) {
    const mapped = character.toLowerCase();
    lowered += [...mapped].length === 1 ? mapped : character === '\u0130' ? 'i' : character;
  }
  return lowered;
}

// RFC 5322 atext as Go's net/mail isAtext: visible ASCII except the specials, and every non-ASCII character.
const atom = '[!#-\'*+\\-/-9=?A-Z^-~\\u0080-\\u{10FFFF}]+';
const dotAtom = new RegExp(`^${atom}(?:\\.${atom})*$`, 'u');
const domainLiteral = /^\[([!-Z^-~\u0080-\u{10FFFF}]*)\]$/u;

/**
 * Go's canonicalEmailAddress: the trimmed, lower-cased address when net/mail parses it as exactly one
 * dot-atom addr-spec with a dot-atom domain or an IP domain literal, within 320 bytes; otherwise ''.
 * A quoted local part never matches, since net/mail returns it unquoted.
 */
export function goCanonicalEmailAddress(value: string): string {
  const email = goToLower(goTrimSpace(value));
  if (!email || utf8Length(email) > 320) return '';
  const at = email.lastIndexOf('@');
  if (at <= 0) return '';
  const local = email.slice(0, at), domain = email.slice(at + 1);
  if (!dotAtom.test(local)) return '';
  if (dotAtom.test(domain)) return email;
  const literal = domainLiteral.exec(domain)?.[1];
  // Go's net.ParseIP accepts IPv4 and IPv6 text without a zone.
  return literal !== undefined && !literal.includes('%') && isIP(literal) !== 0 ? email : '';
}
