// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package httpconnector

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

type ReplayGuard interface {
	Use(key string, expiresAt time.Time) bool
}

type WebhookVerifier struct {
	Secret  []byte
	MaxSkew time.Duration
	Replay  ReplayGuard
	Now     func() time.Time
}

func SignWebhook(secret []byte, timestamp time.Time, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(timestamp.Unix(), 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (verifier WebhookVerifier) Verify(timestampText, signature string, body []byte) error {
	timestampUnix, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid webhook timestamp")
	}
	now := time.Now
	if verifier.Now != nil {
		now = verifier.Now
	}
	maxSkew := verifier.MaxSkew
	if maxSkew == 0 {
		maxSkew = 5 * time.Minute
	}
	timestamp := time.Unix(timestampUnix, 0)
	if delta := now().Sub(timestamp); delta > maxSkew || delta < -maxSkew {
		return fmt.Errorf("webhook timestamp is outside the allowed window")
	}
	expected, err := hex.DecodeString(SignWebhook(verifier.Secret, timestamp, body))
	if err != nil {
		return fmt.Errorf("compute webhook signature: %w", err)
	}
	actual, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(actual, expected) {
		return fmt.Errorf("invalid webhook signature")
	}
	if verifier.Replay != nil && !verifier.Replay.Use(timestampText+":"+signature, timestamp.Add(maxSkew)) {
		return fmt.Errorf("webhook replay detected")
	}
	return nil
}
