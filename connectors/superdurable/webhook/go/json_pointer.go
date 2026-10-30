// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook

import (
	"errors"
	"strconv"
	"strings"
)

// jsonPointer is a parsed RFC 6901 pointer that names a value below the document root.
type jsonPointer struct {
	referenceTokens []string
}

// parseJSONPointer rejects the root pointer, because an event ID or match value is never the whole body.
func parseJSONPointer(text string) (jsonPointer, error) {
	if !strings.HasPrefix(text, "/") {
		return jsonPointer{}, errors.New("JSON Pointer must start with /")
	}
	encodedTokens := strings.Split(text[1:], "/")
	referenceTokens := make([]string, len(encodedTokens))
	for index, encodedToken := range encodedTokens {
		referenceToken, err := decodeReferenceToken(encodedToken)
		if err != nil {
			return jsonPointer{}, err
		}
		referenceTokens[index] = referenceToken
	}
	return jsonPointer{referenceTokens: referenceTokens}, nil
}

// resolve walks a document decoded with json.Decoder.UseNumber and reports whether the value exists.
func (pointer jsonPointer) resolve(document any) (any, bool) {
	current := document
	for _, referenceToken := range pointer.referenceTokens {
		switch container := current.(type) {
		case map[string]any:
			child, isFound := container[referenceToken]
			if !isFound {
				return nil, false
			}
			current = child
		case []any:
			index, isIndex := arrayIndex(referenceToken)
			if !isIndex || index >= len(container) {
				return nil, false
			}
			current = container[index]
		default:
			return nil, false
		}
	}
	return current, true
}

// decodeReferenceToken applies RFC 6901 escapes: ~1 is "/" and ~0 is "~".
func decodeReferenceToken(encodedToken string) (string, error) {
	if !strings.Contains(encodedToken, "~") {
		return encodedToken, nil
	}
	var decoded strings.Builder
	for index := 0; index < len(encodedToken); index++ {
		if encodedToken[index] != '~' {
			decoded.WriteByte(encodedToken[index])
			continue
		}
		if index+1 == len(encodedToken) || (encodedToken[index+1] != '0' && encodedToken[index+1] != '1') {
			return "", errors.New("JSON Pointer contains ~ without 0 or 1")
		}
		if encodedToken[index+1] == '0' {
			decoded.WriteByte('~')
		} else {
			decoded.WriteByte('/')
		}
		index++
	}
	return decoded.String(), nil
}

// arrayIndex accepts RFC 6901 array indexes: digits without a leading zero.
func arrayIndex(referenceToken string) (int, bool) {
	if referenceToken == "" || (len(referenceToken) > 1 && referenceToken[0] == '0') {
		return 0, false
	}
	for index := 0; index < len(referenceToken); index++ {
		if referenceToken[index] < '0' || referenceToken[index] > '9' {
			return 0, false
		}
	}
	value, err := strconv.Atoi(referenceToken)
	return value, err == nil
}
