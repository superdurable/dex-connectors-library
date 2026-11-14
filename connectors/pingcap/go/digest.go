// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pingcap

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

type digestChallenge struct {
	realm     string
	nonce     string
	opaque    string
	algorithm string
}

func parseDigestChallenge(header string) (digestChallenge, error) {
	if len(header) > 8192 || !strings.HasPrefix(strings.ToLower(header), "digest ") {
		return digestChallenge{}, errors.New("invalid Digest challenge")
	}
	values := map[string]string{}
	remaining := strings.TrimSpace(header[7:])
	for remaining != "" {
		separator := strings.IndexByte(remaining, '=')
		if separator < 1 {
			return digestChallenge{}, errors.New("invalid Digest parameter")
		}
		name := strings.ToLower(strings.TrimSpace(remaining[:separator]))
		if name == "" || values[name] != "" {
			return digestChallenge{}, errors.New("duplicate or empty Digest parameter")
		}
		remaining = strings.TrimSpace(remaining[separator+1:])
		var value string
		if strings.HasPrefix(remaining, "\"") {
			remaining = remaining[1:]
			var builder strings.Builder
			closed := false
			for index := 0; index < len(remaining); index++ {
				character := remaining[index]
				if character == '\\' {
					index++
					if index >= len(remaining) {
						return digestChallenge{}, errors.New("invalid Digest escape")
					}
					builder.WriteByte(remaining[index])
					continue
				}
				if character == '"' {
					remaining = strings.TrimSpace(remaining[index+1:])
					closed = true
					break
				}
				if character < 0x20 || character == 0x7f {
					return digestChallenge{}, errors.New("invalid Digest control character")
				}
				builder.WriteByte(character)
			}
			if !closed {
				return digestChallenge{}, errors.New("unclosed Digest value")
			}
			value = builder.String()
		} else {
			end := strings.IndexByte(remaining, ',')
			if end < 0 {
				value = strings.TrimSpace(remaining)
				remaining = ""
			} else {
				value = strings.TrimSpace(remaining[:end])
				remaining = remaining[end:]
			}
		}
		if value == "" || len(value) > 2048 {
			return digestChallenge{}, errors.New("empty Digest value")
		}
		for _, character := range value {
			if character < 0x20 || character == 0x7f {
				return digestChallenge{}, errors.New("invalid Digest control character")
			}
		}
		values[name] = value
		if remaining != "" {
			if remaining[0] != ',' {
				return digestChallenge{}, errors.New("invalid Digest delimiter")
			}
			remaining = strings.TrimSpace(remaining[1:])
			if remaining == "" {
				return digestChallenge{}, errors.New("trailing Digest delimiter")
			}
		}
	}
	if values["realm"] == "" || values["nonce"] == "" || strings.EqualFold(values["stale"], "true") {
		return digestChallenge{}, errors.New("incomplete or stale Digest challenge")
	}
	algorithm := values["algorithm"]
	if algorithm == "" {
		algorithm = "MD5"
	}
	switch strings.ToUpper(algorithm) {
	case "MD5", "MD5-SESS", "SHA-256", "SHA-256-SESS":
	default:
		return digestChallenge{}, errors.New("unsupported Digest algorithm")
	}
	hasAuth := false
	for _, qop := range strings.Split(values["qop"], ",") {
		if strings.TrimSpace(qop) == "auth" {
			hasAuth = true
		}
	}
	if !hasAuth {
		return digestChallenge{}, errors.New("unsupported Digest qop")
	}
	return digestChallenge{realm: values["realm"], nonce: values["nonce"], opaque: values["opaque"], algorithm: algorithm}, nil
}

func digestAuthorization(challenge digestChallenge, username string, password string, method string, uri string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", errors.New("Digest nonce generation failed")
	}
	cnonce := hex.EncodeToString(random)
	hash := func(value string) string {
		if strings.HasPrefix(strings.ToUpper(challenge.algorithm), "SHA-256") {
			sum := sha256.Sum256([]byte(value))
			return hex.EncodeToString(sum[:])
		}
		sum := md5.Sum([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	ha1 := hash(username + ":" + challenge.realm + ":" + password)
	if strings.HasSuffix(strings.ToUpper(challenge.algorithm), "-SESS") {
		ha1 = hash(ha1 + ":" + challenge.nonce + ":" + cnonce)
	}
	ha2 := hash(method + ":" + uri)
	response := hash(ha1 + ":" + challenge.nonce + ":00000001:" + cnonce + ":auth:" + ha2)
	quote := func(value string) string {
		return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(value) + "\""
	}
	header := "Digest username=" + quote(username) + ", realm=" + quote(challenge.realm) + ", nonce=" + quote(challenge.nonce) + ", uri=" + quote(uri) + ", response=" + quote(response) + ", algorithm=" + challenge.algorithm + ", qop=auth, nc=00000001, cnonce=" + quote(cnonce)
	if challenge.opaque != "" {
		header += ", opaque=" + quote(challenge.opaque)
	}
	return header, nil
}
