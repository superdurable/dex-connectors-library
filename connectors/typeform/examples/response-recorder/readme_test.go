// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	responserecorder "github.com/superdurable/dex-connectors-library/connectors/typeform/examples/response-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly and runs the curl delivery's body.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 3)
	var credentials struct {
		AuthMethod    string `json:"auth_method"`
		AccessToken   string `json:"access_token"`
		WebhookSecret string `json:"webhook_secret"`
	}
	requireStrictJSON(t, samples[0], &credentials)
	require.Equal(t, typeform.PersonalAccessTokenAuthMethodID, credentials.AuthMethod)
	var binding struct {
		ConnectorID    string                                         `json:"connectorId"`
		ConnectionName string                                         `json:"connectionName"`
		TriggerName    string                                         `json:"triggerName"`
		BindingName    string                                         `json:"bindingName"`
		Configuration  typeform.ResponseSubmittedTriggerConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[1], &binding)
	require.Equal(t, typeform.ConnectorID, binding.ConnectorID)
	require.Equal(t, responserecorder.ConnectionName, binding.ConnectionName)
	require.Equal(t, typeform.ResponseSubmittedTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, responserecorder.ResponseSubmittedTriggerBinding, binding.BindingName)
	require.NoError(t, binding.Configuration.Validate())
	var recorded responserecorder.RecordedQuestions
	requireStrictJSON(t, samples[2], &recorded)
	require.Equal(t, typeform.GetFormBranchFound, recorded.Branch)

	curlBody := regexp.MustCompile(`(?m)^body='(.+)'$`).FindStringSubmatch(readme)
	require.Len(t, curlBody, 2)
	event := decodeThroughTheEndpoint(t, curlBody[1])
	require.Equal(t, binding.Configuration.FormID, event.Payload.FormID, "the sample passes the binding filter")
	require.True(t, responserecorder.AcceptSubmission(event))
	form := typeform.Form{ID: event.Payload.FormID}
	for _, question := range recorded.Questions {
		form.Fields = append(form.Fields, question.Field)
	}
	questions, unmatched := responserecorder.PairQuestionsWithAnswers(form, responserecorder.MapToFlowInput(event))
	require.Equal(t, recorded.Questions, questions, "the documented Attribute is what the sample delivery records")
	require.Empty(t, unmatched)
	require.Contains(t, readme, responserecorder.ResolveFlowID(event))
	require.Contains(t, readme, webhookPath)
}

// TestConnectorREADMESnippetIsTheExampleCode keeps the connector README's Go snippet runnable.
func TestConnectorREADMESnippetIsTheExampleCode(t *testing.T) {
	snippets := fencedBlocks(readFile(t, filepath.Join("..", "..", "README.md")), "go")
	require.Len(t, snippets, 1)
	require.Contains(t, readFile(t, "main.go"), snippets[0])
}

// decodeThroughTheEndpoint signs body as Typeform does and returns the event the binding receives.
func decodeThroughTheEndpoint(t *testing.T, body string) sdkgo.TriggerEvent[typeform.FormResponseEvent] {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "typeform", Name: responserecorder.ConnectionName}
	client, err := typeform.New(typeform.Config{}, sdkgo.StaticCredentialProvider[typeform.Credentials]{reference: {
		AuthMethodID: typeform.PersonalAccessTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
		WebhookSecret: sdkgo.NewSecretString(sentinelSecret),
	}})
	require.NoError(t, err)
	connection, err := typeform.NewConnection(client, reference)
	require.NoError(t, err)
	events := make(chan sdkgo.TriggerEvent[typeform.FormResponseEvent], 1)
	runner := typeform.NewResponseSubmittedTrigger(typeform.ResponseSubmittedTriggerConfig{
		Connection: connection, ConnectionName: responserecorder.ConnectionName, BindingName: responserecorder.ResponseSubmittedTriggerBinding,
		Target: sdkgo.TriggerTargetFunc[typeform.FormResponseEvent](func(_ context.Context, event sdkgo.TriggerEvent[typeform.FormResponseEvent]) error {
			events <- event
			return nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	defer func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	}()
	handler, err := connection.ResponseSubmittedWebhookHandler()
	require.NoError(t, err)
	require.Eventually(t, func() bool { return handler.(interface{ RunningSourceCount() int }).RunningSourceCount() == 1 },
		5*time.Second, time.Millisecond)
	mac := hmac.New(sha256.New, []byte(sentinelSecret))
	mac.Write([]byte(body))
	request := httptest.NewRequest(http.MethodPost, webhookPath, strings.NewReader(body))
	request.Header.Set("Typeform-Signature", "sha256="+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("the README delivery produced no event")
		return sdkgo.TriggerEvent[typeform.FormResponseEvent]{}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}

func fencedBlocks(markdown string, language string) []string {
	var blocks []string
	var lines []string
	isInBlock := false
	for _, line := range strings.Split(markdown, "\n") {
		switch {
		case !isInBlock && line == "```"+language:
			isInBlock, lines = true, nil
		case isInBlock && line == "```":
			isInBlock = false
			blocks = append(blocks, strings.Join(lines, "\n"))
		case isInBlock:
			lines = append(lines, line)
		}
	}
	return blocks
}

func requireStrictJSON(t *testing.T, sample string, value any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(sample))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(value), sample)
}
