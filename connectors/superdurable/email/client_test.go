// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validConfig() email.Config {
	return email.Config{IMAPHost: "imap.fastmail.com", SMTPHost: "smtp.fastmail.com"}
}

func TestNewAppliesDefaultsAndRejectsUnusableConfiguration(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[email.Credentials]{}
	_, err := email.New(validConfig(), credentials)
	require.NoError(t, err, "ports and TLS modes default to implicit TLS on 993 and 465")
	require.Equal(t, email.Config{IMAPPort: 993, IMAPSecurity: email.IMAPSecurityImplicitTLS, SMTPPort: 465, SMTPSecurity: email.SMTPSecurityImplicitTLS},
		email.DefaultConfig())

	for name, change := range map[string]func(*email.Config){
		"blank IMAP host":          func(config *email.Config) { config.IMAPHost = "" },
		"IMAP host with a scheme":  func(config *email.Config) { config.IMAPHost = "imaps://imap.fastmail.com" },
		"SMTP host with a port":    func(config *email.Config) { config.SMTPHost = "smtp.fastmail.com:465" },
		"port out of range":        func(config *email.Config) { config.SMTPPort = 70000 },
		"unknown security":         func(config *email.Config) { config.IMAPSecurity = "none" },
		"display-name fromAddress": func(config *email.Config) { config.FromAddress = "Support <support@example.com>" },
		"fromName with line break": func(config *email.Config) { config.FromName = "Acme\r\nBcc: x@example.com" },
	} {
		config := validConfig()
		change(&config)
		_, err := email.New(config, credentials)
		require.Error(t, err, name)
	}
	_, err = email.New(validConfig(), nil)
	require.Error(t, err)
	_, err = email.New(validConfig(), credentials, nil)
	require.Error(t, err)
}

func TestOperationsSelectDefectForUnusableCredentialsOrSender(t *testing.T) {
	fixture := newMailFixture(t)
	withLineBreak := fixture.clientWithCredentials(t, email.Credentials{Username: testUsername, Password: sdkgo.NewSecretString("pass\r\nword")})
	result, err := sdkgo.RunQuery(newEmailDexContext("credentials"), withLineBreak.SearchMessages(), emailConnection, email.SearchMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, email.SearchMessagesBranchDefect, result.Branch)
	require.Equal(t, "connection credentials are unavailable", result.Failure.Message)
	require.NotContains(t, fmt.Sprintf("%+v", result), "pass")

	bareLogin := fixture.clientWithCredentials(t, email.Credentials{Username: "support", Password: sdkgo.NewSecretString(testPassword)})
	send, err := sdkgo.RunMutation(newEmailDexContext("sender"), bareLogin.SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchDefect, send.Branch)
	require.Contains(t, send.Failure.Message, "fromAddress is blank")
	require.Zero(t, fixture.smtp.MailCount())
}

func TestDecodeResolvedCredentialsJSONAcceptsOnlyTheDeclaredFields(t *testing.T) {
	credentials, err := email.DecodeResolvedCredentialsJSON(json.RawMessage(`{"username":"support@example.com","password":"` + testPassword + `","smtp_username":"relay"}`))
	require.NoError(t, err)
	require.Equal(t, "support@example.com", credentials.Username)
	require.Equal(t, testPassword, credentials.Password.Reveal())
	require.Equal(t, "relay", credentials.SMTPUsername)
	require.Empty(t, credentials.SMTPPassword.Reveal())
	for _, invalid := range []string{
		`{"username":"support@example.com"}`,
		`{"username":"support@example.com","password":"` + testPassword + `","api_key":"x"}`,
		`{"username":"support@example.com","password":"line\nbreak-` + testPassword + `"}`,
	} {
		_, err := email.DecodeResolvedCredentialsJSON(json.RawMessage(invalid))
		require.Error(t, err)
		require.NotContains(t, err.Error(), testPassword)
	}
}

func TestCredentialsAndConnectionsNeverSerializeSecrets(t *testing.T) {
	credentials := email.Credentials{Username: testUsername, Password: sdkgo.NewSecretString(testPassword)}
	require.NotContains(t, fmt.Sprintf("%+v %#v", credentials, credentials), testPassword)
	_, err := json.Marshal(credentials)
	require.Error(t, err)
	client, err := email.New(validConfig(), sdkgo.StaticCredentialProvider[email.Credentials]{emailConnection: credentials})
	require.NoError(t, err)
	connection, err := email.NewConnection(client, emailConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.Equal(t, "email.Connection{[REDACTED]}", fmt.Sprint(connection))
}
