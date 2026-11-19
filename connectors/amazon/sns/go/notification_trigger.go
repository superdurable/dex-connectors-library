// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

var signingCertificatePathPattern = regexp.MustCompile(`^/SimpleNotificationService-[a-fA-F0-9]{32}\.pem$`)

// NotificationReceivedTriggerConfiguration selects exactly one SNS topic.
type NotificationReceivedTriggerConfiguration struct {
	// TopicARN is copied from SNS Console > Topics and must match the connection Region.
	TopicARN string `json:"topicARN"`
}

// SNSNotification is one RSA signature-verified SNS message.
type SNSNotification struct {
	// MessageID is the provider-assigned stable notification ID.
	MessageID string `json:"messageID"`
	// TopicARN is the publishing topic.
	TopicARN string `json:"topicARN"`
	// Subject is the optional email subject.
	Subject string `json:"subject,omitempty"`
	// Message is the UTF-8 publisher payload.
	Message string `json:"message"`
	// PublishedAt is the SNS signed timestamp.
	PublishedAt time.Time `json:"publishedAt"`
}

type snsWebhookMessage struct {
	Type             string  `json:"Type"`
	MessageID        string  `json:"MessageId"`
	TopicARN         string  `json:"TopicArn"`
	Message          string  `json:"Message"`
	Subject          *string `json:"Subject,omitempty"`
	Timestamp        string  `json:"Timestamp"`
	SignatureVersion string  `json:"SignatureVersion"`
	Signature        string  `json:"Signature"`
	SigningCertURL   string  `json:"SigningCertURL"`
	SubscribeURL     string  `json:"SubscribeURL,omitempty"`
	Token            string  `json:"Token,omitempty"`
}

type snsNotificationEndpoint struct {
	endpoint     *webhooktrigger.Endpoint[Credentials, SNSNotification]
	client       *Client
	mu           sync.RWMutex
	activeTopics map[string]int
}

type snsNotificationSource struct {
	endpoint *snsNotificationEndpoint
	topicARN string
	source   sdkgo.TriggerSource[SNSNotification]
}

// Validate checks that the binding selects one topic in the connection Region.
func (configuration NotificationReceivedTriggerConfiguration) Validate() error {
	if !topicARNPattern.MatchString(configuration.TopicARN) {
		return errors.New("notificationReceived topicARN is invalid")
	}
	return nil
}

// NotificationReceivedWebhookHandler returns the HTTPS endpoint to mount at a public route.
// SNS must be subscribed to the route; signed confirmation messages are accepted automatically.
func (connection Connection) NotificationReceivedWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	endpoint, err := connection.client.notificationReceivedWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	return endpoint.endpoint, nil
}

func (client *Client) notificationReceivedTriggerSource(connection sdkgo.ConnectionRef, configuration NotificationReceivedTriggerConfiguration) sdkgo.TriggerSource[SNSNotification] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	if !topicARNInRegion(configuration.TopicARN, client.region) {
		panic("SNS Trigger topic region must match the connection Region")
	}
	endpoint, err := client.notificationReceivedWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	source := endpoint.endpoint.NewSource(func(event sdkgo.TriggerEvent[SNSNotification]) bool {
		return event.Payload.TopicARN == configuration.TopicARN
	})
	return &snsNotificationSource{endpoint: endpoint, topicARN: configuration.TopicARN, source: source}
}

func (source *snsNotificationSource) Run(ctx context.Context, target sdkgo.TriggerTarget[SNSNotification]) error {
	source.endpoint.mu.Lock()
	source.endpoint.activeTopics[source.topicARN]++
	source.endpoint.mu.Unlock()
	defer func() {
		source.endpoint.mu.Lock()
		source.endpoint.activeTopics[source.topicARN]--
		if source.endpoint.activeTopics[source.topicARN] == 0 {
			delete(source.endpoint.activeTopics, source.topicARN)
		}
		source.endpoint.mu.Unlock()
	}()
	return source.source.Run(ctx, target)
}

func (client *Client) notificationReceivedWebhookEndpoint(connection sdkgo.ConnectionRef) (*snsNotificationEndpoint, error) {
	client.notificationEndpointsMu.Lock()
	defer client.notificationEndpointsMu.Unlock()
	if endpoint, found := client.notificationEndpoints[connection]; found {
		return endpoint, nil
	}
	endpointState := &snsNotificationEndpoint{client: client, activeTopics: map[string]int{}}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, SNSNotification]{
		ConnectorID: ConnectorID, TriggerName: NotificationReceivedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, MaxBodyBytes: 1 << 20,
		VerifyRequestContext: endpointState.verifyRequest, DecodeEvent: decodeSNSNotification, Now: client.now,
	})
	if err != nil {
		return nil, fmt.Errorf("SNS notification endpoint: %w", err)
	}
	endpointState.endpoint = endpoint
	client.notificationEndpoints[connection] = endpointState
	return endpointState, nil
}

func (endpoint *snsNotificationEndpoint) verifyRequest(ctx context.Context, request webhooktrigger.Request, _ Credentials) error {
	var message snsWebhookMessage
	if json.Unmarshal(request.Body, &message) != nil || !validSNSMessage(message) || !topicARNInRegion(message.TopicARN, endpoint.client.region) {
		return errors.New("SNS notification is invalid")
	}
	if request.Header.Get("X-Amz-Sns-Topic-Arn") != message.TopicARN || request.Header.Get("X-Amz-Sns-Message-Type") != message.Type || request.Header.Get("X-Amz-Sns-Message-Id") != message.MessageID {
		return errors.New("SNS notification headers do not match the signed body")
	}
	endpoint.mu.RLock()
	isActive := endpoint.activeTopics[message.TopicARN] > 0
	endpoint.mu.RUnlock()
	if !isActive {
		return errors.New("SNS topic is not bound to an active Trigger")
	}
	certificateURL, err := endpoint.client.validSNSURL(message.SigningCertURL, true)
	if err != nil {
		return err
	}
	certificate, err := endpoint.client.fetchSigningCertificate(ctx, certificateURL)
	if err != nil {
		return fmt.Errorf("%w: SNS signing certificate unavailable", webhooktrigger.ErrVerificationUnavailable)
	}
	if err := verifySNSMessageSignature(message, certificate); err != nil {
		return err
	}
	if message.Type == "SubscriptionConfirmation" {
		confirmationURL, err := endpoint.client.validSNSURL(message.SubscribeURL, false)
		if err != nil || confirmationURL.Query().Get("Action") != "ConfirmSubscription" || confirmationURL.Query().Get("TopicArn") != message.TopicARN || confirmationURL.Query().Get("Token") != message.Token {
			return errors.New("SNS confirmation URL is invalid")
		}
		if err := endpoint.client.confirmSNSSubscription(ctx, confirmationURL); err != nil {
			return fmt.Errorf("%w: SNS subscription confirmation unavailable", webhooktrigger.ErrVerificationUnavailable)
		}
	}
	return nil
}

func validSNSMessage(message snsWebhookMessage) bool {
	if message.Type != "Notification" && message.Type != "SubscriptionConfirmation" && message.Type != "UnsubscribeConfirmation" {
		return false
	}
	if message.MessageID == "" || len(message.MessageID) > 128 || !topicARNPattern.MatchString(message.TopicARN) || message.Message == "" || message.Timestamp == "" || message.Signature == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, message.Timestamp); err != nil {
		return false
	}
	if message.SignatureVersion != "1" && message.SignatureVersion != "2" {
		return false
	}
	if message.Type != "Notification" && (message.SubscribeURL == "" || message.Token == "") {
		return false
	}
	return true
}

func topicARNInRegion(topicARN, region string) bool {
	return strings.Contains(topicARN, ":sns:"+region+":")
}

func (client *Client) validSNSURL(rawURL string, certificate bool) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	expectedHost := "sns." + client.region + ".amazonaws.com"
	if strings.HasPrefix(client.region, "cn-") {
		expectedHost += ".cn"
	}
	if err != nil || parsed.Scheme != "https" || parsed.Host != expectedHost || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("SNS URL is outside the selected regional HTTPS endpoint")
	}
	if certificate {
		if parsed.RawQuery != "" || !signingCertificatePathPattern.MatchString(parsed.Path) {
			return nil, errors.New("SNS signing certificate URL is invalid")
		}
	} else if parsed.Path != "/" || parsed.Query().Get("Action") != "ConfirmSubscription" {
		return nil, errors.New("SNS confirmation URL is invalid")
	}
	return parsed, nil
}

func (client *Client) fetchSigningCertificate(ctx context.Context, certificateURL *url.URL) (*x509.Certificate, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, certificateURL.String(), nil)
	if err != nil {
		return nil, err
	}
	noRedirect := *client.httpClient
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := noRedirect.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("SNS signing certificate request failed")
	}
	certificatePEM, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(certificatePEM) > 64*1024 {
		return nil, errors.New("SNS signing certificate is too large")
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("SNS signing certificate is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	if _, err := certificate.Verify(x509.VerifyOptions{Roots: client.certificateRoots, CurrentTime: client.now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return nil, err
	}
	if !strings.Contains(strings.Join(certificate.Subject.Organization, " "), "Amazon") {
		return nil, errors.New("SNS signing certificate issuer is invalid")
	}
	return certificate, nil
}

func (client *Client) confirmSNSSubscription(ctx context.Context, confirmationURL *url.URL) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, confirmationURL.String(), nil)
	if err != nil {
		return err
	}
	noRedirect := *client.httpClient
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := noRedirect.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("SNS confirmation request failed")
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return err
}

func verifySNSMessageSignature(message snsWebhookMessage, certificate *x509.Certificate) error {
	publicKey, valid := certificate.PublicKey.(*rsa.PublicKey)
	if !valid {
		return errors.New("SNS signing certificate key is not RSA")
	}
	signature, err := base64.StdEncoding.DecodeString(message.Signature)
	if err != nil {
		return errors.New("SNS signature is invalid")
	}
	canonical := canonicalSNSMessage(message)
	var digest []byte
	var algorithm crypto.Hash
	if message.SignatureVersion == "2" {
		hash := sha256.Sum256([]byte(canonical))
		digest = hash[:]
		algorithm = crypto.SHA256
	} else {
		hash := sha1.Sum([]byte(canonical))
		digest = hash[:]
		algorithm = crypto.SHA1
	}
	if rsa.VerifyPKCS1v15(publicKey, algorithm, digest, signature) != nil {
		return errors.New("SNS signature does not match")
	}
	return nil
}

func canonicalSNSMessage(message snsWebhookMessage) string {
	var contents strings.Builder
	add := func(key, value string) {
		contents.WriteString(key)
		contents.WriteByte('\n')
		contents.WriteString(value)
		contents.WriteByte('\n')
	}
	add("Message", message.Message)
	add("MessageId", message.MessageID)
	if message.Type == "Notification" {
		if message.Subject != nil {
			add("Subject", *message.Subject)
		}
	} else {
		add("SubscribeURL", message.SubscribeURL)
	}
	add("Timestamp", message.Timestamp)
	if message.Type != "Notification" {
		add("Token", message.Token)
	}
	add("TopicArn", message.TopicARN)
	add("Type", message.Type)
	return contents.String()
}

func decodeSNSNotification(request webhooktrigger.Request) (sdkgo.TriggerEvent[SNSNotification], bool, error) {
	var message snsWebhookMessage
	if err := json.Unmarshal(request.Body, &message); err != nil || !validSNSMessage(message) {
		return sdkgo.TriggerEvent[SNSNotification]{}, false, errors.New("SNS notification JSON is invalid")
	}
	if message.Type != "Notification" {
		return sdkgo.TriggerEvent[SNSNotification]{}, false, nil
	}
	publishedAt, err := time.Parse(time.RFC3339Nano, message.Timestamp)
	if err != nil {
		return sdkgo.TriggerEvent[SNSNotification]{}, false, err
	}
	event := SNSNotification{MessageID: message.MessageID, TopicARN: message.TopicARN, Message: message.Message, PublishedAt: publishedAt.UTC()}
	if message.Subject != nil {
		event.Subject = *message.Subject
	}
	return sdkgo.TriggerEvent[SNSNotification]{ID: message.MessageID, OccurredAt: event.PublishedAt, Payload: event}, true, nil
}
