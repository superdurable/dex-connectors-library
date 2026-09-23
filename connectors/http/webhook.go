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

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

type WebhookRequest struct {
	Timestamp string `json:"timestamp"`
	Signature string `json:"signature"`
	Body      []byte `json:"body"`
}

type WebhookResult struct {
	Verified bool `json:"verified"`
}

type VerifyWebhookOperation struct {
	client *Client
}

func (VerifyWebhookOperation) Definition() connector.QueryDefinition {
	return VerifyWebhookDefinition
}

func (operation VerifyWebhookOperation) Invoke(call connector.Call, input WebhookRequest) connector.QueryAttempt[WebhookResult] {
	if input.Timestamp == "" || input.Signature == "" || len(input.Body) == 0 {
		failure := connector.Failure{Kind: connector.FailureValidation, Provider: "http", Operation: "verifyWebhook", Message: "timestamp, signature, and body are required"}
		return connector.NewQueryBranch(VerifyWebhookBranchDefect, WebhookResult{}, &failure, connector.Receipt{})
	}
	if operation.client.webhookReplay == nil {
		failure := connector.Failure{Kind: connector.FailureLocalDefect, Provider: "http", Operation: "verifyWebhook", Message: "webhook replay protection is not configured"}
		return connector.NewQueryBranch(VerifyWebhookBranchDefect, WebhookResult{}, &failure, connector.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.WebhookSecret.Reveal() == "" {
		failure := connector.Failure{Kind: connector.FailureAuthentication, Provider: "http", Operation: "verifyWebhook", Message: "webhook credentials are unavailable"}
		return connector.NewQueryBranch(VerifyWebhookBranchRejected, WebhookResult{}, &failure, connector.Receipt{})
	}
	verifier := WebhookVerifier{
		Secret: []byte(credentials.WebhookSecret.Reveal()), Replay: operation.client.webhookReplay, Now: operation.client.now,
	}
	if err := verifier.Verify(input.Timestamp, input.Signature, input.Body); err != nil {
		failure := connector.Failure{Kind: connector.FailureProviderRejection, Provider: "http", Operation: "verifyWebhook", Message: "webhook verification failed"}
		return connector.NewQueryBranch(VerifyWebhookBranchRejected, WebhookResult{}, &failure, connector.Receipt{})
	}
	return connector.NewQueryBranch(VerifyWebhookBranchVerified, WebhookResult{Verified: true}, nil, connector.Receipt{})
}

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
