// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go/deskmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func newTriggerTestConnection(t *testing.T, pollInterval time.Duration) (desk.Connection, error) {
	t.Helper()
	client, err := desk.New(desk.Config{OrgID: testOrganizationID, PollInterval: pollInterval}, sdkgo.StaticCredentialProvider[desk.Credentials]{
		deskConnection: {AuthMethodID: desk.USDataCenterAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken)},
	})
	if err != nil {
		return desk.Connection{}, err
	}
	return desk.NewConnection(client, deskConnection)
}

func ignoreTicketChange(context.Context, sdkgo.TriggerEvent[desk.TicketChangedEvent]) error {
	return nil
}

func TestPollIntervalHasAFloorAndADefault(t *testing.T) {
	_, err := newTriggerTestConnection(t, 5*time.Second)
	require.ErrorContains(t, err, "pollInterval must be at least 15s")
	require.Equal(t, time.Minute, desk.DefaultConfig().PollInterval)
	_, err = newTriggerTestConnection(t, 0)
	require.NoError(t, err, "a blank interval uses the default")
}

func TestTicketTriggerRunnerValidatesItsRoutes(t *testing.T) {
	connection, err := newTriggerTestConnection(t, 15*time.Second)
	require.NoError(t, err)
	target := sdkgo.TriggerTargetFunc[desk.TicketChangedEvent](ignoreTicketChange)

	_, err = desk.NewTicketTriggerRunner(desk.TicketTriggerRunnerConfig{Connection: connection})
	require.ErrorContains(t, err, "routes are required")
	_, err = desk.NewTicketTriggerRunner(desk.TicketTriggerRunnerConfig{Connection: connection, TicketChangedRoutes: []desk.TicketChangedTriggerRoute{{
		BindingName: "ticket-changes", Configuration: desk.TicketChangedTriggerConfiguration{DepartmentID: "Billing"}, Target: target,
	}}})
	require.ErrorContains(t, err, "departmentId must be a numeric Zoho Desk ID")
	_, err = desk.NewTicketTriggerRunner(desk.TicketTriggerRunnerConfig{Connection: connection, TicketChangedRoutes: []desk.TicketChangedTriggerRoute{{BindingName: " ", Target: target}}})
	require.ErrorContains(t, err, "binding name and target are required")
	runner, err := desk.NewTicketTriggerRunner(desk.TicketTriggerRunnerConfig{Connection: connection, TicketChangedRoutes: []desk.TicketChangedTriggerRoute{{BindingName: "ticket-changes", Target: target}}})
	require.NoError(t, err)
	require.NotNil(t, runner)

	mocked := deskmock.New(t, deskConnection.Name).Connection()
	_, err = desk.NewTicketTriggerRunner(desk.TicketTriggerRunnerConfig{Connection: mocked, TicketChangedRoutes: []desk.TicketChangedTriggerRoute{{BindingName: "ticket-changes", Target: target}}})
	require.ErrorContains(t, err, "serves only operation Steps")
}

func TestGeneratedTicketChangedTriggerBindsTheConnection(t *testing.T) {
	connection, err := newTriggerTestConnection(t, 15*time.Second)
	require.NoError(t, err)
	trigger := desk.NewTicketChangedTrigger(desk.TicketChangedTriggerConfig{
		Connection: connection, ConnectionName: deskConnection.Name, BindingName: "ticket-changes",
		Target: sdkgo.TriggerTargetFunc[desk.TicketChangedEvent](ignoreTicketChange),
	})
	require.NotNil(t, trigger)
	require.Panics(t, func() {
		desk.NewTicketChangedTrigger(desk.TicketChangedTriggerConfig{
			Connection: connection, ConnectionName: deskConnection.Name, BindingName: "ticket-changes",
			Configuration: desk.TicketChangedTriggerConfiguration{DepartmentID: "Billing"},
			Target:        sdkgo.TriggerTargetFunc[desk.TicketChangedEvent](ignoreTicketChange),
		})
	})
}
