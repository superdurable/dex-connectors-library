// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package graphtest

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const graphTimeLayout = "2006-01-02T15:04:05Z"

// graphRoute is one parsed Graph request below the mailbox root.
type graphRoute struct {
	endpoint  Endpoint
	folder    string
	messageID string
	action    string
}

// serveGraph routes /v1.0/me/... and /v1.0/users/{mailbox}/... after recording the request.
func (server *Server) serveGraph(writer http.ResponseWriter, request *http.Request, segments []string, body []byte) {
	if len(segments) < 2 || segments[0] != "v1.0" {
		writeGraphError(writer, http.StatusNotFound, "BadRequest")
		return
	}
	rest, isMe, mailbox := segments[2:], segments[1] == "me", ""
	if segments[1] == "users" && len(segments) > 2 {
		rest, mailbox = segments[3:], segments[2]
	} else if !isMe {
		writeGraphError(writer, http.StatusNotFound, "BadRequest")
		return
	}
	route, ok := routeMailboxRequest(request.Method, rest)
	if !ok {
		writeGraphError(writer, http.StatusNotFound, "ResourceNotFound")
		return
	}
	fault := server.recordRequest(route.endpoint, request, body)
	if server.applyFaultBeforeHandling(writer, fault) {
		return
	}
	faultWriter := faultAnsweringWriter{ResponseWriter: writer, fault: fault}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if status, code := server.authorizeGraphRequest(request, isMe, mailbox); status != 0 {
		writeGraphError(writer, status, code)
		return
	}
	server.mailbox.completeDueSends(server.now())
	status, value, header := server.handleMailboxRequest(route, request, body)
	faultWriter.answerInsteadOf(func(responseWriter http.ResponseWriter) {
		for name, values := range header {
			responseWriter.Header()[name] = values
		}
		switch {
		case status >= 400:
			writeGraphError(responseWriter, status, value.(string))
		case value == nil:
			responseWriter.Header().Set("request-id", "0d5e3b4f-1a2b-4c3d-8e9f-001122334455")
			responseWriter.WriteHeader(status)
		default:
			writeJSON(responseWriter, status, value)
		}
	})
}

func routeMailboxRequest(method string, rest []string) (graphRoute, bool) {
	switch {
	case len(rest) == 2 && rest[0] == "mailFolders" && method == http.MethodGet:
		return graphRoute{endpoint: EndpointGetMailFolder, folder: rest[1]}, true
	case len(rest) == 3 && rest[0] == "mailFolders" && rest[2] == "messages" && method == http.MethodGet:
		return graphRoute{endpoint: EndpointListFolderMessages, folder: rest[1]}, true
	case len(rest) == 1 && rest[0] == "messages" && method == http.MethodGet:
		return graphRoute{endpoint: EndpointFindMessagesByMarker}, true
	case len(rest) == 1 && rest[0] == "messages" && method == http.MethodPost:
		return graphRoute{endpoint: EndpointCreateDraft}, true
	case len(rest) == 2 && rest[0] == "messages" && method == http.MethodGet:
		return graphRoute{endpoint: EndpointGetMessage, messageID: rest[1]}, true
	case len(rest) == 2 && rest[0] == "messages" && method == http.MethodPatch:
		return graphRoute{endpoint: EndpointUpdateMessage, messageID: rest[1]}, true
	case len(rest) == 3 && rest[0] == "messages" && rest[2] == "attachments" && method == http.MethodGet:
		return graphRoute{endpoint: EndpointListAttachments, messageID: rest[1]}, true
	case len(rest) == 3 && rest[0] == "messages" && (rest[2] == "createReply" || rest[2] == "createReplyAll") && method == http.MethodPost:
		return graphRoute{endpoint: EndpointCreateReply, messageID: rest[1], action: rest[2]}, true
	case len(rest) == 3 && rest[0] == "messages" && rest[2] == "send" && method == http.MethodPost:
		return graphRoute{endpoint: EndpointSendDraft, messageID: rest[1]}, true
	case len(rest) == 3 && rest[0] == "messages" && rest[2] == "move" && method == http.MethodPost:
		return graphRoute{endpoint: EndpointMoveMessage, messageID: rest[1]}, true
	default:
		return graphRoute{}, false
	}
}

// authorizeGraphRequest checks the token, the mailbox the token may reach, and the immutable-ID preference.
func (server *Server) authorizeGraphRequest(request *http.Request, isMe bool, mailbox string) (int, string) {
	token, isBearer := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	grant, isKnown := server.tokens[token]
	if !isBearer || !isKnown || grant.isExpired {
		return http.StatusUnauthorized, "InvalidAuthenticationToken"
	}
	switch {
	case isMe && grant.isAppOnly:
		return http.StatusBadRequest, "BadRequest"
	case !isMe && !strings.EqualFold(mailbox, server.mailbox.address):
		if grant.isAppOnly {
			return http.StatusForbidden, "ErrorAccessDenied"
		}
		return http.StatusNotFound, "ErrorInvalidUser"
	}
	if !strings.Contains(strings.Join(request.Header.Values("Prefer"), ","), `IdType="ImmutableId"`) {
		return http.StatusBadRequest, "FakeGraphImmutableIdRequired"
	}
	return 0, ""
}

// handleMailboxRequest applies one request; an error status carries its error code as the value.
func (server *Server) handleMailboxRequest(route graphRoute, request *http.Request, body []byte) (int, any, http.Header) {
	mailbox := server.mailbox
	query := request.URL.Query()
	selected := selectedProperties(query.Get("$select"))
	prefersText := strings.Contains(strings.Join(request.Header.Values("Prefer"), ","), `outlook.body-content-type="text"`) && !server.ignoresTextBodyPreference
	switch route.endpoint {
	case EndpointGetMailFolder:
		folder := mailbox.resolveFolder(route.folder)
		if folder == nil {
			return http.StatusNotFound, "ErrorItemNotFound", nil
		}
		return http.StatusOK, mailbox.renderFolder(folder, selected), nil
	case EndpointListFolderMessages:
		folder := mailbox.resolveFolder(route.folder)
		if folder == nil {
			return http.StatusNotFound, "ErrorItemNotFound", nil
		}
		return server.listMessages(request, query, selected, func(message *messageState) bool { return message.folderID == folder.id })
	case EndpointFindMessagesByMarker:
		return server.listMessages(request, query, selected, func(*messageState) bool { return true })
	case EndpointCreateDraft:
		return server.createDraft(body, selected)
	}
	message, ok := mailbox.messages[route.messageID]
	if !ok {
		return http.StatusNotFound, "ErrorItemNotFound", nil
	}
	switch route.endpoint {
	case EndpointGetMessage:
		var header http.Header
		if prefersText {
			header = http.Header{"Preference-Applied": {`outlook.body-content-type="text"`}}
		}
		return http.StatusOK, mailbox.renderMessage(message, selected, prefersText), header
	case EndpointListAttachments:
		return http.StatusOK, renderAttachments(message, selected), nil
	case EndpointCreateReply:
		return server.createReply(message, route.action == "createReplyAll", body)
	case EndpointUpdateMessage:
		return server.updateMessage(message, body)
	case EndpointSendDraft:
		if request.Header.Get("Content-Length") != "0" {
			return http.StatusLengthRequired, "ErrorMissingContentLength", nil
		}
		mailbox.deliveries = append(mailbox.deliveries, Delivery{
			MessageID: message.id, Subject: message.subject, Recipients: message.recipients(), Body: message.body,
		})
		if message.isDraft && server.sendCompletionDelay > 0 {
			completesAt := server.now().Add(server.sendCompletionDelay)
			message.sendCompletesAt = &completesAt
		} else if message.isDraft {
			mailbox.completeSend(message, server.now())
		}
		return http.StatusAccepted, nil, nil
	default:
		return server.moveMessage(message, body)
	}
}

// listMessages filters, orders newest first, and pages with $skip, as Graph's message collections do.
func (server *Server) listMessages(request *http.Request, query url.Values, selected map[string]bool, include func(*messageState) bool) (int, any, http.Header) {
	filter, err := parseMessageFilter(query.Get("$filter"))
	if err != nil {
		return http.StatusBadRequest, "BadRequest", nil
	}
	if orderBy := query.Get("$orderby"); orderBy != "" {
		if orderBy != "receivedDateTime desc" {
			return http.StatusBadRequest, "BadRequest", nil
		}
		if filter.firstProperty != "" && filter.firstProperty != "receivedDateTime" {
			return http.StatusBadRequest, "InefficientFilter", nil
		}
	}
	top, skip := 10, 0
	if value := query.Get("$top"); value != "" {
		if top, err = strconv.Atoi(value); err != nil || top < 1 || top > 1000 {
			return http.StatusBadRequest, "BadRequest", nil
		}
	}
	if value := query.Get("$skip"); value != "" {
		if skip, err = strconv.Atoi(value); err != nil || skip < 0 {
			return http.StatusBadRequest, "BadRequest", nil
		}
	}
	matches := server.mailbox.sortedMessages(func(message *messageState) bool { return include(message) && filter.matches(message) })
	page := []map[string]any{}
	for index := skip; index < len(matches) && index < skip+top; index++ {
		page = append(page, server.mailbox.renderMessage(matches[index], selected, false))
	}
	envelope := map[string]any{"value": page}
	if skip+top < len(matches) {
		next := url.Values{}
		for name, values := range query {
			next[name] = values
		}
		next.Set("$skip", strconv.Itoa(skip+top))
		envelope["@odata.nextLink"] = "https://graph.microsoft.com" + request.URL.EscapedPath() + "?" + strings.ReplaceAll(next.Encode(), "+", "%20")
	}
	return http.StatusOK, envelope, nil
}

// draftWire is the part of a message body the fake reads from create, createReply, and update requests.
type draftWire struct {
	Subject *string `json:"subject"`
	Body    *struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	ToRecipients  []recipientWire `json:"toRecipients"`
	CcRecipients  []recipientWire `json:"ccRecipients"`
	BccRecipients []recipientWire `json:"bccRecipients"`
	ReplyTo       []recipientWire `json:"replyTo"`
	IsRead        *bool           `json:"isRead"`
	Flag          *struct {
		FlagStatus string `json:"flagStatus"`
	} `json:"flag"`
	Categories                    *[]string `json:"categories"`
	SingleValueExtendedProperties []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"singleValueExtendedProperties"`
}

type recipientWire struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

func (server *Server) createDraft(body []byte, selected map[string]bool) (int, any, http.Header) {
	var draft draftWire
	if err := json.Unmarshal(body, &draft); err != nil || draft.Subject == nil || draft.Body == nil {
		return http.StatusBadRequest, "RequestBodyRead", nil
	}
	mailbox := server.mailbox
	message := mailbox.newMessage(mailbox.wellKnown["drafts"], server.now())
	message.isDraft, message.isRead = true, true
	message.subject, message.body, message.bodyContentType = *draft.Subject, draft.Body.Content, strings.ToLower(draft.Body.ContentType)
	message.from = &addressState{address: mailbox.address}
	message.to, message.cc, message.bcc, message.replyTo = recipientStates(draft.ToRecipients), recipientStates(draft.CcRecipients),
		recipientStates(draft.BccRecipients), recipientStates(draft.ReplyTo)
	for _, property := range draft.SingleValueExtendedProperties {
		message.extendedProperties[property.ID] = property.Value
	}
	return http.StatusCreated, mailbox.renderMessage(message, nil, false), nil
}

func (server *Server) createReply(original *messageState, isReplyAll bool, body []byte) (int, any, http.Header) {
	var parameters struct {
		Comment string     `json:"comment"`
		Message *draftWire `json:"message"`
	}
	if err := json.Unmarshal(body, &parameters); err != nil {
		return http.StatusBadRequest, "RequestBodyRead", nil
	}
	mailbox := server.mailbox
	reply := mailbox.newMessage(mailbox.wellKnown["drafts"], server.now())
	reply.isDraft, reply.isRead, reply.conversationID = true, true, original.conversationID
	reply.from = &addressState{address: mailbox.address}
	reply.subject = original.subject
	if !strings.HasPrefix(strings.ToUpper(reply.subject), "RE:") {
		reply.subject = "RE: " + reply.subject
	}
	reply.to = append([]addressState(nil), original.replyTo...)
	if len(reply.to) == 0 && original.from != nil {
		reply.to = []addressState{*original.from}
	}
	if isReplyAll {
		reply.to = append(reply.to, withoutAddress(original.to, mailbox.address)...)
		reply.cc = withoutAddress(original.cc, mailbox.address)
	}
	reply.bodyContentType = "html"
	reply.body = "<html><body><p>" + html.EscapeString(parameters.Comment) + "</p><hr><div>" + html.EscapeString(original.plainText()) + "</div></body></html>"
	return http.StatusCreated, mailbox.renderMessage(reply, nil, false), nil
}

func (server *Server) updateMessage(message *messageState, body []byte) (int, any, http.Header) {
	var update draftWire
	if err := json.Unmarshal(body, &update); err != nil {
		return http.StatusBadRequest, "RequestBodyRead", nil
	}
	if len(update.SingleValueExtendedProperties) > 0 && !message.isDraft {
		return http.StatusBadRequest, "ErrorInvalidPropertyUpdateSentMessage", nil
	}
	if update.IsRead != nil {
		message.isRead = *update.IsRead
	}
	if update.Flag != nil {
		switch update.Flag.FlagStatus {
		case "notFlagged", "flagged", "complete":
			message.flagStatus = update.Flag.FlagStatus
		default:
			return http.StatusBadRequest, "ErrorInvalidRequest", nil
		}
	}
	if update.Categories != nil {
		message.categories = append([]string{}, *update.Categories...)
	}
	for _, property := range update.SingleValueExtendedProperties {
		message.extendedProperties[property.ID] = property.Value
	}
	return http.StatusOK, server.mailbox.renderMessage(message, nil, false), nil
}

func (server *Server) moveMessage(message *messageState, body []byte) (int, any, http.Header) {
	var parameters struct {
		DestinationID string `json:"destinationId"`
	}
	if err := json.Unmarshal(body, &parameters); err != nil {
		return http.StatusBadRequest, "RequestBodyRead", nil
	}
	folder := server.mailbox.resolveFolder(parameters.DestinationID)
	if folder == nil {
		return http.StatusNotFound, "ErrorItemNotFound", nil
	}
	message.folderID = folder.id
	return http.StatusCreated, server.mailbox.renderMessage(message, nil, false), nil
}

// renderMessage writes Graph's message JSON, keeping only selected properties when $select is present.
func (mailbox *mailboxState) renderMessage(message *messageState, selected map[string]bool, prefersText bool) map[string]any {
	body := map[string]any{"contentType": message.bodyContentType, "content": message.body}
	if prefersText {
		body = map[string]any{"contentType": "text", "content": message.plainText()}
	}
	preview := message.plainText()
	if len([]rune(preview)) > 255 {
		preview = string([]rune(preview)[:255])
	}
	rendered := map[string]any{
		"@odata.etag": "W/\"etag\"", "id": message.id, "conversationId": message.conversationID,
		"internetMessageId": message.internetMessageID, "parentFolderId": message.folderID, "subject": message.subject,
		"from": recipientValue(message.from), "sender": recipientValue(message.from), "replyTo": recipientValues(message.replyTo),
		"toRecipients": recipientValues(message.to), "ccRecipients": recipientValues(message.cc), "bccRecipients": recipientValues(message.bcc),
		"receivedDateTime": message.receivedAt.Format(graphTimeLayout), "createdDateTime": message.createdAt.Format(graphTimeLayout),
		"isRead": message.isRead, "isDraft": message.isDraft, "flag": map[string]any{"flagStatus": message.flagStatus},
		"categories": append([]string{}, message.categories...), "importance": "normal",
		"hasAttachments": hasFileAttachments(message), "bodyPreview": preview, "body": body,
		"webLink": "https://outlook.office365.com/owa/?ItemID=" + url.QueryEscape(message.id),
	}
	if message.sentAt != nil {
		rendered["sentDateTime"] = message.sentAt.Format(graphTimeLayout)
	} else {
		rendered["sentDateTime"] = nil
	}
	if selected == nil {
		return rendered
	}
	filtered := map[string]any{"@odata.etag": rendered["@odata.etag"], "id": message.id}
	for name := range selected {
		if value, ok := rendered[name]; ok {
			filtered[name] = value
		}
	}
	return filtered
}

func (mailbox *mailboxState) renderFolder(folder *folderState, selected map[string]bool) map[string]any {
	children := 0
	for _, candidate := range mailbox.folders {
		if candidate.parentID == folder.id {
			children++
		}
	}
	rendered := map[string]any{"id": folder.id, "displayName": folder.displayName, "parentFolderId": folder.parentID, "childFolderCount": children}
	if selected == nil {
		return rendered
	}
	filtered := map[string]any{"id": folder.id}
	for name := range selected {
		if value, ok := rendered[name]; ok {
			filtered[name] = value
		}
	}
	return filtered
}

// renderAttachments includes contentBytes unless $select leaves it out, as Graph does.
func renderAttachments(message *messageState, selected map[string]bool) map[string]any {
	items := []map[string]any{}
	for _, attachment := range message.attachments {
		kind := firstNonEmpty(attachment.Kind, "file")
		item := map[string]any{
			"@odata.type": "#microsoft.graph." + kind + "Attachment", "id": attachment.id, "name": attachment.Name,
			"contentType": attachment.ContentType, "size": attachment.Size, "isInline": attachment.IsInline,
		}
		if selected == nil || selected["contentBytes"] {
			item["contentBytes"] = strings.Repeat("QUJD", int(min(attachment.Size, 4096)/4))
		}
		items = append(items, item)
	}
	return map[string]any{"value": items}
}

func hasFileAttachments(message *messageState) bool {
	for _, attachment := range message.attachments {
		if !attachment.IsInline {
			return true
		}
	}
	return false
}

func selectedProperties(selectList string) map[string]bool {
	if selectList == "" {
		return nil
	}
	selected := map[string]bool{}
	for _, name := range strings.Split(selectList, ",") {
		selected[strings.TrimSpace(name)] = true
	}
	return selected
}

func recipientValue(address *addressState) any {
	if address == nil {
		return nil
	}
	return map[string]any{"emailAddress": map[string]any{"name": address.name, "address": address.address}}
}

func recipientValues(list []addressState) []any {
	values := []any{}
	for index := range list {
		values = append(values, recipientValue(&list[index]))
	}
	return values
}

func recipientStates(wires []recipientWire) []addressState {
	var states []addressState
	for _, wire := range wires {
		states = append(states, addressState{name: wire.EmailAddress.Name, address: wire.EmailAddress.Address})
	}
	return states
}

func withoutAddress(list []addressState, excluded string) []addressState {
	var kept []addressState
	for _, item := range list {
		if !strings.EqualFold(item.address, excluded) {
			kept = append(kept, item)
		}
	}
	return kept
}
