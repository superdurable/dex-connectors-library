// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateCalendarRetriesAmbiguousProviderResponses(t *testing.T) {
	for _, response := range []struct {
		name   string
		status int
		body   string
	}{
		{"conflict", http.StatusConflict, `{"error":{"code":"conflict"}}`},
		{"unlisted status", http.StatusMethodNotAllowed, `{"error":{"code":"methodNotAllowed"}}`},
		{"empty refusal", http.StatusBadRequest, ``},
		{"unavailable", http.StatusServiceUnavailable, `{"error":{"code":"serviceUnavailable"}}`},
		{"invalid success", http.StatusCreated, `{"id":""}`},
	} {
		t.Run(response.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(writer http.ResponseWriter, request *http.Request, _ int) {
				writeJSON(t, writer, response.status, response.body)
			})
			_, err := sdkgo.RunMutation(singledispatchtest.NewContext("calendar-ambiguous", "create"), newCalendarClient(t, provider.URL).CreateCalendar(), calendarConnection, outlookcalendar.CreateCalendarInput{Name: "Audit"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		})
	}
}
