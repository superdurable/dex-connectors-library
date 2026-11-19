// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

type notificationRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip notificationRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestSNSNotificationSignatureAndConfirmation(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{Organization: []string{"Amazon Web Services"}, CommonName: "SNS test root"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	require.NoError(t, err)
	root, err := x509.ParseCertificate(rootDER)
	require.NoError(t, err)
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signingTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{Organization: []string{"Amazon Simple Notification Service"}, CommonName: "SNS test signing"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
	}
	signingDER, err := x509.CreateCertificate(rand.Reader, signingTemplate, root, &signingKey.PublicKey, rootKey)
	require.NoError(t, err)
	signing, err := x509.ParseCertificate(signingDER)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signingDER})
	certificateURL := "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-11111111111111111111111111111111.pem"
	topicARN := "arn:aws:sns:eu-west-1:123456789012:alerts"
	confirmationURL := "https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription&TopicArn=" + topicARN + "&Token=test-token"
	var requests []string
	client := &Client{
		region: "eu-west-1", certificateRoots: roots, now: func() time.Time { return now },
		httpClient: &http.Client{Transport: notificationRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests = append(requests, request.URL.String())
			body := string(certificatePEM)
			if request.URL.Query().Get("Action") == "ConfirmSubscription" {
				body = "<ConfirmSubscriptionResponse/>"
			}
			return &http.Response{StatusCode: http.StatusOK, Body: ioNopCloser(body), Header: http.Header{}}, nil
		})},
	}
	endpoint := &snsNotificationEndpoint{client: client, activeTopics: map[string]int{topicARN: 1}}
	message := snsWebhookMessage{
		Type: "Notification", MessageID: "11111111-1111-4111-8111-111111111111", TopicARN: topicARN,
		Message: "Report ready", Timestamp: now.Format(time.RFC3339Nano), SignatureVersion: "2", SigningCertURL: certificateURL,
	}
	signMessage := func(message *snsWebhookMessage) {
		digest := sha256.Sum256([]byte(canonicalSNSMessage(*message)))
		signature, err := rsa.SignPKCS1v15(rand.Reader, signingKey, crypto.SHA256, digest[:])
		require.NoError(t, err)
		message.Signature = base64.StdEncoding.EncodeToString(signature)
	}
	requestFor := func(message snsWebhookMessage) webhooktrigger.Request {
		body, err := json.Marshal(message)
		require.NoError(t, err)
		return webhooktrigger.Request{Body: body, ReceivedAt: now, Header: http.Header{
			"X-Amz-Sns-Topic-Arn":    []string{message.TopicARN},
			"X-Amz-Sns-Message-Type": []string{message.Type},
			"X-Amz-Sns-Message-Id":   []string{message.MessageID},
		}}
	}
	signMessage(&message)
	require.NoError(t, verifySNSMessageSignature(message, signing))
	require.NoError(t, endpoint.verifyRequest(context.Background(), requestFor(message), Credentials{}))
	event, handled, err := decodeSNSNotification(requestFor(message))
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, message.MessageID, event.ID)
	require.Equal(t, "Report ready", event.Payload.Message)
	require.Len(t, requests, 1)
	tampered := message
	tampered.Message = "Other message"
	require.Error(t, endpoint.verifyRequest(context.Background(), requestFor(tampered), Credentials{}))
	require.Len(t, requests, 2)
	confirmation := message
	confirmation.Type = "SubscriptionConfirmation"
	confirmation.MessageID = "22222222-2222-4222-8222-222222222222"
	confirmation.Message = "Please confirm."
	confirmation.SubscribeURL = confirmationURL
	confirmation.Token = "test-token"
	signMessage(&confirmation)
	require.NoError(t, endpoint.verifyRequest(context.Background(), requestFor(confirmation), Credentials{}))
	_, handled, err = decodeSNSNotification(requestFor(confirmation))
	require.NoError(t, err)
	require.False(t, handled)
	require.Equal(t, confirmationURL, requests[len(requests)-1])
	delete(endpoint.activeTopics, topicARN)
	require.Error(t, endpoint.verifyRequest(context.Background(), requestFor(message), Credentials{}))
	require.Equal(t, len(requests), 4)
}

func TestSNSNotificationRejectsExternalSigningLocations(t *testing.T) {
	client := &Client{region: "eu-west-1"}
	for _, location := range []string{
		"http://sns.eu-west-1.amazonaws.com/SimpleNotificationService-11111111111111111111111111111111.pem",
		"https://sns.eu-west-1.amazonaws.com.evil.example/SimpleNotificationService-11111111111111111111111111111111.pem",
		"https://sns.eu-west-1.amazonaws.com/other.pem",
		"https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-11111111111111111111111111111111.pem?redirect=1",
	} {
		_, err := client.validSNSURL(location, true)
		require.Error(t, err, location)
	}
}

func ioNopCloser(body string) *stringReaderCloser {
	return &stringReaderCloser{Reader: strings.NewReader(body)}
}

type stringReaderCloser struct{ *strings.Reader }

func (*stringReaderCloser) Close() error { return nil }
