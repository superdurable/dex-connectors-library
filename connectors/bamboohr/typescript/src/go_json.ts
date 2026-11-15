// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * A JSON value that keeps object members in source order, duplicates included, and number text as written,
 * so decoders can fill a structure as Go's encoding/json does: keys match case-insensitively and the last
 * duplicate wins.
 */
export type OrderedJSONValue =
  | { kind: 'object'; members: Array<[string, OrderedJSONValue]> }
  | { kind: 'array'; items: OrderedJSONValue[] }
  | { kind: 'string'; value: string }
  | { kind: 'number'; text: string }
  | { kind: 'boolean'; value: boolean }
  | { kind: 'null' };

const numberPattern = /-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/y;
// JSON strings forbid raw control characters, as Go's scanner does.
// eslint-disable-next-line no-control-regex
const stringPattern = /"(?:[^"\\\u0000-\u001f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"/y;
const maximumDepth = 10_000;

/** Parse one complete JSON document, as json.Unmarshal does; throws for any other text. */
export function parseOrderedJSON(text: string): OrderedJSONValue {
  const { value, end } = parseFirstOrderedJSONValue(text);
  if (!/^[ \t\r\n]*$/.test(text.slice(end))) throw new SyntaxError('invalid character after top-level value');
  return value;
}

/** Parse the first JSON value of text, as a json.Decoder's Decode does; text after it is not read. */
export function parseFirstOrderedJSONValue(text: string): { value: OrderedJSONValue; end: number } {
  const parser = new OrderedJSONParser(text);
  const value = parser.parseValue(0);
  return { value, end: parser.position };
}

/** Select names from an object's members as Go fills a struct: case-insensitive, the last match wins. */
export function selectGoFields(members: Array<[string, OrderedJSONValue]>, names: readonly string[]): Map<string, OrderedJSONValue> {
  const folded = new Map(names.map(name => [name.toLowerCase(), name]));
  const selected = new Map<string, OrderedJSONValue>();
  for (const [key, value] of members) {
    const name = folded.get(key.toLowerCase());
    if (name !== undefined) selected.set(name, value);
  }
  return selected;
}

class OrderedJSONParser {
  position = 0;
  constructor(private readonly text: string) {}

  parseValue(depth: number): OrderedJSONValue {
    if (depth > maximumDepth) throw new SyntaxError('exceeded max depth');
    this.skipWhitespace();
    const character = this.text[this.position];
    if (character === '{') return this.parseObject(depth);
    if (character === '[') return this.parseArray(depth);
    if (character === '"') return { kind: 'string', value: this.parseString() };
    for (const [literal, value] of [['true', { kind: 'boolean', value: true }], ['false', { kind: 'boolean', value: false }], ['null', { kind: 'null' }]] as const) {
      if (this.text.startsWith(literal, this.position)) { this.position += literal.length; return value; }
    }
    numberPattern.lastIndex = this.position;
    const number = numberPattern.exec(this.text);
    if (!number) throw new SyntaxError(`invalid character at offset ${this.position}`);
    this.position += number[0].length;
    return { kind: 'number', text: number[0] };
  }

  private parseObject(depth: number): OrderedJSONValue {
    this.position++;
    const members: Array<[string, OrderedJSONValue]> = [];
    this.skipWhitespace();
    if (this.text[this.position] === '}') { this.position++; return { kind: 'object', members }; }
    for (;;) {
      this.skipWhitespace();
      if (this.text[this.position] !== '"') throw new SyntaxError('expected an object key');
      const key = this.parseString();
      this.expect(':');
      members.push([key, this.parseValue(depth + 1)]);
      this.skipWhitespace();
      const separator = this.text[this.position++];
      if (separator === '}') return { kind: 'object', members };
      if (separator !== ',') throw new SyntaxError('expected , or }');
    }
  }

  private parseArray(depth: number): OrderedJSONValue {
    this.position++;
    const items: OrderedJSONValue[] = [];
    this.skipWhitespace();
    if (this.text[this.position] === ']') { this.position++; return { kind: 'array', items }; }
    for (;;) {
      items.push(this.parseValue(depth + 1));
      this.skipWhitespace();
      const separator = this.text[this.position++];
      if (separator === ']') return { kind: 'array', items };
      if (separator !== ',') throw new SyntaxError('expected , or ]');
    }
  }

  private parseString(): string {
    stringPattern.lastIndex = this.position;
    const literal = stringPattern.exec(this.text);
    if (!literal) throw new SyntaxError('invalid string');
    this.position += literal[0].length;
    // Go decodes a lone surrogate escape as U+FFFD.
    return (JSON.parse(literal[0]) as string).replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '�');
  }

  private expect(character: string): void {
    this.skipWhitespace();
    if (this.text[this.position++] !== character) throw new SyntaxError(`expected ${character}`);
  }

  private skipWhitespace(): void {
    while (this.position < this.text.length && ' \t\r\n'.includes(this.text[this.position]!)) this.position++;
  }
}
