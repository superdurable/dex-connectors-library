// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { buildMIMEMessage, decodeGmailMessage, GmailMessageResourceSchema, parseMailbox, replyRecipient, validateSendInput } from '../src/index.js';
import type { Message } from '../src/models.js';
import { sentMailCandidateQuery } from '../src/sent_mail_read_back.js';

// RFC 2047 section 5(3) admits only these characters in an encoded word that replaces a phrase word.
const PhraseEncodedWord = /^=\?utf-8\?([bq])\?([A-Za-z0-9!*+\-/=_]*)\?=$/;

// The Go connector's go/mailbox_format_test.go pins the same names; conformance replays them against Go.
const displayNames = [
  'Zoë (Ops), Inc', 'Zoë "Q" Ops', 'Zoë\\Ops', 'Zoë <Ops>', 'Zoë@Ops: Team; Leads. [EU]', "Renée O'Brien #1 & {Ops}", 'Zoë/Ops+1-2*3!=?_',
  'Doe, "JD" \\ John', 'Zoë Ångström-Øresund Ελληνικά Кириллица 中文 🎉 Party Planning Committee of the Year',
  'Zoë Ångström (Øresund), Ελληνικά & Кириллица: 中文 🎉 Party Planning Committee of the Year',
  'Zoë\\Ops Ångström-Øresund Ελληνικά Кириллица 中文 Party Planning', '日本語の名前とても長い名前です日本語の名前とても長い名前です',
  'Team 👩‍💻 👨🏽‍🚀', 'שלום Zoë مرحبا',
];

const quoteDisplayName = (name: string) => `"${name.replaceAll('\\', '\\\\').replaceAll('"', '\\"')}" <zoe@example.com>`;

const formatRecipient = (recipient: string) => validateSendInput({ to: [recipient], subject: 'Progress', textBody: 'Update' }, 'owner@example.com')[0]!;

const decodeToHeader = (value: string) => decodeGmailMessage(GmailMessageResourceSchema.parse({
  id: 'message-1', threadId: 'thread-1', internalDate: '1000', payload: { headers: [{ name: 'To', value }] },
})).to;

// Decodes a phrase of adjacent encoded words without the connector's parser.
function decodeEncodedPhrase(phrase: string): string {
  return Buffer.concat(phrase.split(' ').map(word => {
    const [, encoding, text] = PhraseEncodedWord.exec(word) ?? [];
    if (!text) throw new Error(`not a phrase encoded word: ${word}`);
    if (encoding === 'b') return Buffer.from(text, 'base64');
    return Buffer.from(text.replaceAll('_', ' ').replace(/=([0-9A-F]{2})/g, (_match, hex: string) => String.fromCharCode(parseInt(hex, 16))), 'latin1');
  })).toString('utf8');
}

describe('display name To: mailboxes', () => {
  it('writes Go\'s mail.Address.String mailbox, with a backslash B-encoded', () => {
    expect(formatRecipient(quoteDisplayName('Zoë (Ops), Inc'))).toBe('=?utf-8?b?Wm/DqyAoT3BzKSwgSW5j?= <zoe@example.com>');
    expect(formatRecipient(quoteDisplayName('Zoë\\Ops'))).toBe('=?utf-8?b?Wm/Dq1xPcHM=?= <zoe@example.com>');
    expect(formatRecipient(quoteDisplayName('Zoë/Ops+1-2*3!=?_'))).toBe('=?utf-8?q?Zo=C3=AB/Ops+1-2*3!=3D=3F=5F?= <zoe@example.com>');
    expect(formatRecipient(quoteDisplayName('Doe, "JD" \\ John'))).toBe('"Doe, \\"JD\\" \\\\ John" <zoe@example.com>');
    expect(formatRecipient(quoteDisplayName('Team 👩‍💻 👨🏽‍🚀'))).toBe(
      '=?utf-8?q?Team_=F0=9F=91=A9=E2=80=8D=F0=9F=92=BB_=F0=9F=91=A8=F0=9F=8F=BD?= =?utf-8?q?=E2=80=8D=F0=9F=9A=80?= <zoe@example.com>');
  });

  for (const name of displayNames) {
    it(`parses ${JSON.stringify(name)} back to exactly one mailbox`, () => {
      const mailbox = formatRecipient(quoteDisplayName(name));
      expect(mailbox.endsWith(' <zoe@example.com>')).toBe(true);
      const phrase = mailbox.slice(0, -' <zoe@example.com>'.length);
      if (!phrase.startsWith('"')) {
        for (const word of phrase.split(' ')) {
          expect(word.length, word).toBeLessThanOrEqual(75);
          expect(word).toMatch(PhraseEncodedWord);
        }
        expect(decodeEncodedPhrase(phrase)).toBe(name);
      }
      expect(parseMailbox(mailbox)).toEqual({ display: name, address: 'zoe@example.com' });
      expect(decodeToHeader(`${mailbox}, other@example.com`)).toEqual([mailbox, '<other@example.com>']);
      expect(replyRecipient({ replyTo: mailbox, from: 'sender@example.com' } as Message)).toBe(mailbox);
      expect(sentMailCandidateQuery([mailbox])).toBe('in:sent newer_than:2d to:zoe@example.com');
      const raw = Buffer.from(buildMIMEMessage('call-1', 'key', 'id@dex.superdurable.dev', 'owner@example.com', [mailbox, '<other@example.com>'], 'Progress', 'Update')).toString();
      expect(raw).toContain(`\r\nTo: ${mailbox}, <other@example.com>\r\n`);
    });
  }
});

describe('display name decoding', () => {
  it('decodes encoded words as Go\'s net/mail does', () => {
    expect(parseMailbox('=?UTF-8?B?WsO2IChPcHMpLCBJbmM=?= <zoe@example.com>')).toEqual({ display: 'Zö (Ops), Inc', address: 'zoe@example.com' });
    expect(parseMailbox('=?utf-8?q?Zo=C3=AB?= Ops <zoe@example.com>')?.display).toBe('Zoë Ops');
    expect(parseMailbox('=?utf-8?q?Zo=C3=AB?= =?utf-8?q?_Ops?= <zoe@example.com>')?.display).toBe('Zoë Ops');
    expect(parseMailbox('=?utf-8?q?Zo=C3=AB_?=   =?utf-8?q?Ops?= <zoe@example.com>')?.display).toBe('Zoë Ops');
    expect(parseMailbox('Ada   Lovelace <ada@example.com>')?.display).toBe('Ada Lovelace');
    expect(parseMailbox('Zoë\tOps <zoe@example.com>')?.display).toBe('Zoë Ops');
    expect(parseMailbox('"Ada  Lovelace" <ada@example.com>')?.display).toBe('Ada  Lovelace');
    expect(parseMailbox('=?iso-8859-1?q?Z=F6e?= <zoe@example.com>')?.display).toBe('Zöe');
    expect(parseMailbox('=?us-ascii?q?Z=F6e?= <zoe@example.com>')?.display).toBe('Z�e');
    expect(parseMailbox('=?UTF-8?Q?Ren=C3=A9e_O=27Brien?= <renee@example.com>')?.display).toBe("Renée O'Brien");
    expect(parseMailbox('=?koi8-r?b?8M/Q?= <zoe@example.com>')?.display).toBe('Поп');
  });

  it('keeps an invalid or quoted encoded word literal and rejects a name that decodes to nothing', () => {
    expect(parseMailbox('=?utf-8?q?bad=ZZ?= <zoe@example.com>')?.display).toBe('=?utf-8?q?bad=ZZ?=');
    expect(parseMailbox('=?utf-8?b?WsO2?=x <zoe@example.com>')?.display).toBe('=?utf-8?b?WsO2?=x');
    expect(parseMailbox('=?utf-8?b?WsO2?= <zoe@example.com>')?.display).toBe('Zö');
    expect(parseMailbox('=?utf-8?b?WsO2?== <zoe@example.com>')?.display).toBe('=?utf-8?b?WsO2?==');
    expect(parseMailbox('=?x-unknown?q?Z?= <zoe@example.com>')?.display).toBe('=?x-unknown?q?Z?=');
    expect(parseMailbox('"=?utf-8?q?Zo=C3=AB?=" <zoe@example.com>')?.display).toBe('=?utf-8?q?Zo=C3=AB?=');
    expect(parseMailbox('=?utf-8?q??= <zoe@example.com>')).toBeUndefined();
    expect(parseMailbox('=?utf-8?q??= Ops <zoe@example.com>')?.display).toBe('Ops');
    expect(parseMailbox('=?utf-8?q?Zo=C3=AB\\Ops?= <zoe@example.com>')).toBeUndefined();
    expect(parseMailbox('Zoë\x01Ops <zoe@example.com>')).toBeUndefined();
  });

  it('splits a To header only at commas outside quoted strings', () => {
    expect(decodeToHeader('"Doe, \\"JD\\" \\\\ John" <j@example.com>, "x\\"y" <xy@example.com>')).toEqual([
      '"Doe, \\"JD\\" \\\\ John" <j@example.com>', '"x\\"y" <xy@example.com>',
    ]);
    expect(decodeToHeader('"Doe, John" <j@example.com>, not-an-address')).toBeUndefined();
  });
});
