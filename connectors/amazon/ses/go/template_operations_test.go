// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestEmailTemplateReadListUpdateAndDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Contains(t, request.Header.Get("Authorization"), "/ses/aws4_request")
		switch request.Method + " " + request.URL.Path {
		case "GET /v2/email/templates/weekly-report":
			_, err := response.Write([]byte(`{"TemplateName":"weekly-report","TemplateContent":{"Subject":"Weekly report","Text":"Hello {{name}}"}}`))
			require.NoError(t, err)
		case "GET /v2/email/templates":
			require.Equal(t, "2", request.URL.Query().Get("PageSize"))
			_, err := response.Write([]byte(`{"TemplatesMetadata":[{"TemplateName":"weekly-report","CreatedTimestamp":1792918800}]}`))
			require.NoError(t, err)
		case "PUT /v2/email/templates/weekly-report":
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"TemplateContent":{"Subject":"Updated","Text":"Hello"}}`, string(body))
		case "DELETE /v2/email/templates/weekly-report":
		default:
			t.Fatalf("unexpected template request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	got, err := sdkgo.RunQuery(queryContext{}, client.GetEmailTemplate(), testConnection, GetEmailTemplateInput{TemplateName: "weekly-report"})
	require.NoError(t, err)
	require.Equal(t, GetEmailTemplateBranchFound, got.Branch)
	require.Equal(t, "Weekly report", got.Value.Content.Subject)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListEmailTemplates(), testConnection, ListEmailTemplatesInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, ListEmailTemplatesBranchPage, page.Branch)
	require.Equal(t, "weekly-report", page.Value.Templates[0].Name)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateEmailTemplate(), testConnection, UpdateEmailTemplateInput{TemplateName: "weekly-report", Content: EmailTemplateContent{Subject: "Updated", Text: "Hello"}})
	require.NoError(t, err)
	require.Equal(t, UpdateEmailTemplateBranchUpdated, updated.Branch)
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteEmailTemplate(), testConnection, DeleteEmailTemplateInput{TemplateName: "weekly-report"})
	require.NoError(t, err)
	require.Equal(t, DeleteEmailTemplateBranchDeleted, deleted.Branch)
}

func TestCreateEmailTemplateReadsBackAfterUnknownOutcome(t *testing.T) {
	var posts, reads int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts++
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		reads++
		_, err := response.Write([]byte(`{"TemplateContent":{"Subject":"Weekly report","Text":"Hello {{name}}"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	context := &conformanceSendContext{attempt: 1}
	input := CreateEmailTemplateInput{TemplateName: "weekly-report", Content: EmailTemplateContent{Subject: "Weekly report", Text: "Hello {{name}}"}}
	_, err := sdkgo.RunMutation(context, client.CreateEmailTemplate(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	context.attempt = 2
	result, err := sdkgo.RunMutation(context, client.CreateEmailTemplate(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateEmailTemplateBranchCreated, result.Branch)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, reads)
}

func TestSendTemplatedEmailSignsTemplateAndData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/email/outbound-emails", request.URL.Path)
		require.Contains(t, request.Header.Get("Authorization"), "/ses/aws4_request")
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var payload struct {
			Content struct {
				Template struct {
					TemplateName string `json:"TemplateName"`
					TemplateData string `json:"TemplateData"`
				} `json:"Template"`
			} `json:"Content"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "weekly-report", payload.Content.Template.TemplateName)
		require.JSONEq(t, `{"name":"Ada"}`, payload.Content.Template.TemplateData)
		_, err = response.Write([]byte(`{"MessageId":"template-message-1"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := SendTemplatedEmailInput{FromEmailAddress: "sender@example.com", ToEmailAddress: "reader@example.com", TemplateName: "weekly-report", TemplateData: map[string]string{"name": "Ada"}}
	result, err := sdkgo.RunMutation(&conformanceSendContext{attempt: 1}, newTestClient(t, server.URL, 0).SendTemplatedEmail(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendTemplatedEmailBranchSent, result.Branch)
	require.Equal(t, "template-message-1", result.Value.MessageID)
}
