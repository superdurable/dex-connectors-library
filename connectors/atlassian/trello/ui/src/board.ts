// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

const boardIDPattern = /^[0-9a-fA-F]{24}$/;
const shortLinkPattern = /^[0-9A-Za-z]{8}$/;
const boardURLPattern = /^https:\/\/trello\.com\/b\/([0-9A-Za-z]{8})(?:[/?#].*)?$/;

/**
 * boardIdFromInput returns the value the boardInput unit saves: the 8-character short link from a board
 * link such as https://trello.com/b/AbCd1234/support-queue, or a bare short link or 24-character board ID
 * as entered. It returns undefined for anything else, including a card link (https://trello.com/c/...).
 */
export function boardIdFromInput(input: string): string | undefined {
  const value = input.trim();
  const fromURL = boardURLPattern.exec(value);
  if (fromURL) return fromURL[1];
  if (boardIDPattern.test(value) || shortLinkPattern.test(value)) return value;
  return undefined;
}
