// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// accessTokenRefreshSkew renews a token before WeCom's reported expiry so an in-flight send never carries a stale one.
	accessTokenRefreshSkew = 5 * time.Minute
	// maxAccessTokenBytes is the token length bound in https://developer.work.weixin.qq.com/document/path/91039.
	maxAccessTokenBytes = 512
)

// accessTokenCache keeps one application access token per corp ID and secret fingerprint, so
// connections that share a Client never evict each other's token. WeCom rate-limits gettoken and
// asks callers to cache its two-hour token, so each key admits one exchange at a time.
type accessTokenCache struct {
	mutex sync.Mutex
	slots map[accessTokenCacheKey]*accessTokenSlot
}

// accessTokenCacheKey never holds the raw secret, only its SHA-256 fingerprint.
type accessTokenCacheKey struct {
	corpID            string
	secretFingerprint [sha256.Size]byte
}

// accessTokenSlot fields are guarded by its single-slot semaphore, which also honors cancellation.
type accessTokenSlot struct {
	semaphore chan struct{}
	value     string
	expiresAt time.Time
}

// accessTokenOutcome is a resolved token or the classified reason none is available.
type accessTokenOutcome struct {
	token         string
	failure       sdkgo.Failure
	isRejected    bool
	isLocalDefect bool
	shouldRetry   bool
}

func newAccessTokenCache() *accessTokenCache {
	return &accessTokenCache{slots: map[accessTokenCacheKey]*accessTokenSlot{}}
}

func newAccessTokenCacheKey(corpID, corpSecret string) accessTokenCacheKey {
	return accessTokenCacheKey{corpID: corpID, secretFingerprint: sha256.Sum256([]byte(corpSecret))}
}

// resolve returns the key's cached token or exchanges the application secret for a new one.
func (cache *accessTokenCache) resolve(ctx context.Context, client *Client, operation string, key accessTokenCacheKey, corpSecret string) accessTokenOutcome {
	slot := cache.slotFor(key)
	if !slot.acquire(ctx) {
		return accessTokenOutcome{shouldRetry: true, failure: newFailure(operation, sdkgo.FailureTransport, "WeCom access token request was cancelled before it started")}
	}
	defer slot.release()
	if slot.value != "" && client.now().Before(slot.expiresAt) {
		return accessTokenOutcome{token: slot.value}
	}
	outcome, lifetime := doFetchAccessToken(ctx, client, operation, key.corpID, corpSecret)
	if outcome.token != "" {
		slot.value, slot.expiresAt = outcome.token, client.now().Add(lifetime)
	}
	return outcome
}

// invalidate drops the key's cached token only when it is still the one WeCom rejected.
func (cache *accessTokenCache) invalidate(ctx context.Context, key accessTokenCacheKey, rejectedToken string) {
	slot := cache.slotFor(key)
	if !slot.acquire(ctx) {
		// A cancelled attempt keeps the token; the next rejection of it discards it.
		return
	}
	defer slot.release()
	if slot.value == rejectedToken {
		slot.value, slot.expiresAt = "", time.Time{}
	}
}

// slotFor returns the key's slot, creating it once. Slots are never removed, so every caller of a
// key shares one semaphore; their count is bounded by the distinct credentials one Client serves.
func (cache *accessTokenCache) slotFor(key accessTokenCacheKey) *accessTokenSlot {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if slot, isCached := cache.slots[key]; isCached {
		return slot
	}
	slot := &accessTokenSlot{semaphore: make(chan struct{}, 1)}
	cache.slots[key] = slot
	return slot
}

func (slot *accessTokenSlot) acquire(ctx context.Context) bool {
	select {
	case slot.semaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (slot *accessTokenSlot) release() { <-slot.semaphore }

// doFetchAccessToken calls gettoken once and returns the token with its usable lifetime.
func doFetchAccessToken(ctx context.Context, client *Client, operation, corpID, corpSecret string) (accessTokenOutcome, time.Duration) {
	query := url.Values{"corpid": {corpID}, "corpsecret": {corpSecret}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/cgi-bin/gettoken?"+query.Encode(), nil)
	if err != nil {
		return accessTokenOutcome{isLocalDefect: true, failure: newFailure(operation, sdkgo.FailureLocalDefect, "WeCom access token request could not be built")}, 0
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		// The error embeds the request URL, which carries corpsecret, so only a fixed message is kept.
		return retryAccessToken(operation, sdkgo.FailureTransport, "WeCom access token request failed"), 0
	}
	// Closing a read-only token response cannot change the exchange outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return retryAccessToken(operation, sdkgo.FailureRateLimit, "WeCom rate limited the access token request"), 0
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryAccessToken(operation, sdkgo.FailureAvailability, "WeCom access token service is unavailable"), 0
	}
	if response.StatusCode != http.StatusOK {
		return rejectedAccessToken(operation, sdkgo.FailureProviderRejection, "WeCom rejected the access token request with an unexpected HTTP status"), 0
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return retryAccessToken(operation, sdkgo.FailureResponseTooLarge, "WeCom access token response exceeded maxResponseBytes"), 0
	}
	if err != nil {
		return retryAccessToken(operation, sdkgo.FailureTransport, "WeCom access token response was interrupted"), 0
	}
	var tokenResponse struct {
		AccessToken      string `json:"access_token"`
		ExpiresInSeconds int64  `json:"expires_in"`
	}
	errcode, ok := decodeProviderEnvelope(body, &tokenResponse)
	if !ok {
		return retryAccessToken(operation, sdkgo.FailureProtocol, "WeCom access token response is malformed"), 0
	}
	if isRetryableErrcode(errcode) {
		return retryAccessToken(operation, failureKindForErrcode(errcode), rejectionMessage("WeCom deferred the access token request", errcode)), 0
	}
	if errcode != errcodeSuccess {
		// 40001 and 40013 are never retried: repeated wrong-secret requests can block the caller IP for an hour.
		return rejectedAccessToken(operation, failureKindForErrcode(errcode), rejectionMessage("WeCom rejected the application credentials", errcode)), 0
	}
	token := tokenResponse.AccessToken
	if len(token) > maxAccessTokenBytes || !providerhttp.IsHeaderSafeCredential(token) || tokenResponse.ExpiresInSeconds <= 0 {
		return retryAccessToken(operation, sdkgo.FailureProtocol, "WeCom access token response is incomplete"), 0
	}
	return accessTokenOutcome{token: token}, usableAccessTokenLifetime(time.Duration(tokenResponse.ExpiresInSeconds) * time.Second)
}

// usableAccessTokenLifetime keeps the refresh skew, or half of a lifetime too short for it.
func usableAccessTokenLifetime(reported time.Duration) time.Duration {
	if reported > 2*accessTokenRefreshSkew {
		return reported - accessTokenRefreshSkew
	}
	return reported / 2
}

func retryAccessToken(operation string, kind sdkgo.FailureKind, message string) accessTokenOutcome {
	return accessTokenOutcome{shouldRetry: true, failure: newFailure(operation, kind, message)}
}

func rejectedAccessToken(operation string, kind sdkgo.FailureKind, message string) accessTokenOutcome {
	return accessTokenOutcome{isRejected: true, failure: newFailure(operation, kind, message)}
}
