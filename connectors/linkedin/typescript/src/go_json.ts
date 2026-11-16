// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Go encoding/json field matching for the documents the profileChanged Trigger decodes: object members keep
// their order and duplicates, so a later key overrides an earlier one exactly as Go's json.Unmarshal does.

const members = Symbol('goJSONMembers');

type GoJSONObject = Record<string, unknown> & { [members]?: Array<[string, unknown]> };

/** Parse JSON text like JSON.parse, also recording each object's ordered members. Throws SyntaxError. */
export function parseGoJSON(text: string): unknown {
  let index = 0;
  const fail = (): never => { throw new SyntaxError('invalid JSON'); };
  const skipSpace = () => { while (index < text.length && ' \t\n\r'.includes(text[index]!)) index++; };
  const parseString = (): string => {
    const start = index++;
    while (index < text.length && text[index] !== '"') index += text[index] === '\\' ? 2 : 1;
    if (index >= text.length) fail();
    index++;
    return JSON.parse(text.slice(start, index)) as string;
  };
  const parseValue = (): unknown => {
    skipSpace();
    const character = text[index];
    if (character === '"') return parseString();
    if (character === '{') {
      index++;
      const object: GoJSONObject = {}, ordered: Array<[string, unknown]> = [];
      skipSpace();
      if (text[index] === '}') { index++; } else {
        for (;;) {
          skipSpace();
          if (text[index] !== '"') fail();
          const key = parseString();
          skipSpace();
          if (text[index++] !== ':') fail();
          const value = parseValue();
          ordered.push([key, value]);
          Object.defineProperty(object, key, { value, enumerable: true, configurable: true, writable: true });
          skipSpace();
          const separator = text[index++];
          if (separator === '}') break;
          if (separator !== ',') fail();
        }
      }
      Object.defineProperty(object, members, { value: ordered, enumerable: false });
      return object;
    }
    if (character === '[') {
      index++;
      const array: unknown[] = [];
      skipSpace();
      if (text[index] === ']') { index++; return array; }
      for (;;) {
        array.push(parseValue());
        skipSpace();
        const separator = text[index++];
        if (separator === ']') return array;
        if (separator !== ',') fail();
      }
    }
    const literal = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(text.slice(index))?.[0];
    if (!literal) return fail();
    index += literal.length;
    return JSON.parse(literal) as unknown;
  };
  const value = parseValue();
  skipSpace();
  if (index !== text.length) fail();
  return value;
}

// Go folds key runes with unicode.SimpleFold; only these two non-ASCII runes fold onto ASCII letters.
const foldKey = (key: string) => key.replace(/ſ/g, 's').replace(/K/g, 'k').replace(/[A-Z]/g, letter => letter.toLowerCase());

/**
 * Go's json.Unmarshal field lookup for name: every key equal to name ignoring case assigns in order, and a
 * null leaves a string, bool, number, or struct field unchanged. With isRawMessage, a null also assigns.
 */
export function goField(source: Record<string, unknown>, name: string, isRawMessage = false): unknown {
  const ordered = (source as GoJSONObject)[members] ?? Object.entries(source);
  const folded = foldKey(name);
  let value: unknown;
  for (const [key, member] of ordered) {
    if (foldKey(key) === folded && (isRawMessage || member !== null)) value = member;
  }
  return value;
}
