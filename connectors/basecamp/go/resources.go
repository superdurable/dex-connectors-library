// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Project contains a Basecamp project identity and name.
type Project struct {
	// ID is the numeric project ID.
	ID string `json:"id"`
	// Name is the current project name.
	Name string `json:"name"`
}

// ProjectsPage contains one provider page of active projects.
type ProjectsPage struct {
	// Projects are the visible projects on this page.
	Projects []Project `json:"projects"`
}

// Todo contains a Basecamp to-do identity and completion state.
type Todo struct {
	// ID is the numeric to-do ID.
	ID string `json:"id"`
	// Content is the to-do title.
	Content string `json:"content"`
	// Completed reports whether the to-do is complete.
	Completed bool `json:"completed"`
	// Description is provider rich text for the to-do.
	Description string `json:"description,omitempty"`
	// DueOn is the optional ISO date due date.
	DueOn string `json:"dueOn,omitempty"`
	// StartsOn is the optional ISO start date.
	StartsOn string `json:"startsOn,omitempty"`
	// AssigneeIDs are the people assigned to this to-do.
	AssigneeIDs []string `json:"assigneeIds,omitempty"`
	// CompletionSubscriberIDs are people notified at completion.
	CompletionSubscriberIDs []string `json:"completionSubscriberIds,omitempty"`
}

// TodosPage contains one provider page of pending to-dos.
type TodosPage struct {
	// Todos are the visible to-dos on this page.
	Todos []Todo `json:"todos"`
}

// ListProjectsInput identifies the Basecamp account.
type ListProjectsInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
}

// GetProjectInput identifies one project in an account.
type GetProjectInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// ProjectID comes from listProjects.
	ProjectID string `json:"projectId"`
}

// ListTodosInput identifies a to-do list in an account.
type ListTodosInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoListID comes from the project's to-do set.
	TodoListID string `json:"todoListId"`
}

// GetTodoInput identifies one to-do in an account.
type GetTodoInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// TodoID comes from a to-do list or another Basecamp result.
	TodoID string `json:"todoId"`
}

// ListProjectsOperation reads a page of active projects.
type ListProjectsOperation struct{ client *Client }

// ListProjects binds the project list Query to this client.
func (client *Client) ListProjects() ListProjectsOperation { return ListProjectsOperation{client} }

// Definition returns the generated Query contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke reads the first page of active projects.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ProjectsPage] {
	if !validResourceID(input.AccountID) {
		return resourceBranch[ProjectsPage]("listProjects", ListProjectsBranchDefect, sdkgo.FailureValidation, "accountId is invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/projects.json")
	return resourceQueryAttempt(response, "listProjects", ListProjectsBranchListed, "", ListProjectsBranchProviderRejected, ListProjectsBranchInvalidResponse, ListProjectsBranchDefect, decodeProjects)
}

// GetProjectOperation reads one project.
type GetProjectOperation struct{ client *Client }

// GetProject binds the project Query to this client.
func (client *Client) GetProject() GetProjectOperation { return GetProjectOperation{client} }

// Definition returns the generated Query contract.
func (GetProjectOperation) Definition() sdkgo.QueryDefinition { return GetProjectDefinition }

// Invoke reads the selected project.
func (operation GetProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.QueryAttempt[Project] {
	if !validResourceID(input.AccountID) || !validResourceID(input.ProjectID) {
		return resourceBranch[Project]("getProject", GetProjectBranchDefect, sdkgo.FailureValidation, "accountId or projectId is invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/projects/"+input.ProjectID+".json")
	return resourceQueryAttempt(response, "getProject", GetProjectBranchFound, GetProjectBranchNotFound, GetProjectBranchProviderRejected, GetProjectBranchInvalidResponse, GetProjectBranchDefect, func(body []byte) (Project, error) {
		return decodeProject(body, input.ProjectID)
	})
}

// ListTodosOperation reads one page of pending to-dos.
type ListTodosOperation struct{ client *Client }

// ListTodos binds the to-do list Query to this client.
func (client *Client) ListTodos() ListTodosOperation { return ListTodosOperation{client} }

// Definition returns the generated Query contract.
func (ListTodosOperation) Definition() sdkgo.QueryDefinition { return ListTodosDefinition }

// Invoke reads the selected to-do list.
func (operation ListTodosOperation) Invoke(call sdkgo.Call, input ListTodosInput) sdkgo.QueryAttempt[TodosPage] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoListID) {
		return resourceBranch[TodosPage]("listTodos", ListTodosBranchDefect, sdkgo.FailureValidation, "accountId or todoListId is invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/todolists/"+input.TodoListID+"/todos.json")
	return resourceQueryAttempt(response, "listTodos", ListTodosBranchListed, "", ListTodosBranchProviderRejected, ListTodosBranchInvalidResponse, ListTodosBranchDefect, decodeTodos)
}

// GetTodoOperation reads one to-do.
type GetTodoOperation struct{ client *Client }

// GetTodo binds the to-do Query to this client.
func (client *Client) GetTodo() GetTodoOperation { return GetTodoOperation{client} }

// Definition returns the generated Query contract.
func (GetTodoOperation) Definition() sdkgo.QueryDefinition { return GetTodoDefinition }

// Invoke reads the selected to-do.
func (operation GetTodoOperation) Invoke(call sdkgo.Call, input GetTodoInput) sdkgo.QueryAttempt[Todo] {
	if !validResourceID(input.AccountID) || !validResourceID(input.TodoID) {
		return resourceBranch[Todo]("getTodo", GetTodoBranchDefect, sdkgo.FailureValidation, "accountId or todoId is invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/todos/"+input.TodoID+".json")
	return resourceQueryAttempt(response, "getTodo", GetTodoBranchFound, GetTodoBranchNotFound, GetTodoBranchProviderRejected, GetTodoBranchInvalidResponse, GetTodoBranchDefect, func(body []byte) (Todo, error) {
		return decodeTodo(body, input.TodoID)
	})
}

func resourceQueryAttempt[T any](response providerResponse, operation string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	if errors.Is(response.err, errCredential) {
		return resourceBranch[T](operation, defect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return resourceRetry[T](operation, sdkgo.FailureTransport, "Basecamp request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return resourceRetry[T](operation, sdkgo.FailureRateLimit, "Basecamp rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return resourceRetry[T](operation, sdkgo.FailureAvailability, "Basecamp is temporarily unavailable", response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return resourceBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "Basecamp response exceeded the configured limit")
	}
	if response.err != nil {
		return resourceRetry[T](operation, sdkgo.FailureTransport, "Basecamp response was interrupted", 0)
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return resourceBranch[T](operation, notFound, sdkgo.FailureNotFound, "Basecamp resource does not exist")
	}
	if response.status != http.StatusOK {
		return resourceBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Basecamp rejected HTTP "+strconv.Itoa(response.status))
	}
	value, err := decode(response.body)
	if err != nil {
		return resourceBranch[T](operation, invalid, sdkgo.FailureProtocol, "Basecamp returned invalid resource JSON")
	}
	return sdkgo.NewQueryBranch(success, value, nil, sdkgo.Receipt{})
}

func resourceBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "basecamp", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func resourceRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "basecamp", Operation: operation, Message: message}, delay)
}

func validResourceID(value string) bool {
	if value == "" || len(value) > 16 || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func decodeProjects(body []byte) (ProjectsPage, error) {
	var records []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil {
		return ProjectsPage{}, fmt.Errorf("projects are invalid")
	}
	page := ProjectsPage{Projects: make([]Project, 0, len(records))}
	for _, record := range records {
		if record.ID <= 0 || record.Name == "" {
			return ProjectsPage{}, fmt.Errorf("project is invalid")
		}
		page.Projects = append(page.Projects, Project{ID: strconv.FormatInt(record.ID, 10), Name: record.Name})
	}
	return page, nil
}

func decodeProject(body []byte, expectedID string) (Project, error) {
	var record struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record.ID <= 0 || (expectedID != "" && strconv.FormatInt(record.ID, 10) != expectedID) || record.Name == "" {
		return Project{}, fmt.Errorf("project is invalid")
	}
	return Project{ID: strconv.FormatInt(record.ID, 10), Name: record.Name}, nil
}

func decodeTodos(body []byte) (TodosPage, error) {
	var records []struct {
		ID        int64  `json:"id"`
		Content   string `json:"content"`
		Completed bool   `json:"completed"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil {
		return TodosPage{}, fmt.Errorf("to-dos are invalid")
	}
	page := TodosPage{Todos: make([]Todo, 0, len(records))}
	for _, record := range records {
		if record.ID <= 0 || strings.TrimSpace(record.Content) == "" {
			return TodosPage{}, fmt.Errorf("to-do is invalid")
		}
		page.Todos = append(page.Todos, Todo{ID: strconv.FormatInt(record.ID, 10), Content: record.Content, Completed: record.Completed})
	}
	return page, nil
}

func decodeTodo(body []byte, expectedID string) (Todo, error) {
	var record struct {
		ID          int64  `json:"id"`
		Content     string `json:"content"`
		Completed   bool   `json:"completed"`
		Description string `json:"description"`
		DueOn       string `json:"due_on"`
		StartsOn    string `json:"starts_on"`
		Assignees   []struct {
			ID int64 `json:"id"`
		} `json:"assignees"`
		CompletionSubscribers []struct {
			ID int64 `json:"id"`
		} `json:"completion_subscribers"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record.ID <= 0 || (expectedID != "" && strconv.FormatInt(record.ID, 10) != expectedID) || strings.TrimSpace(record.Content) == "" {
		return Todo{}, fmt.Errorf("to-do is invalid")
	}
	value := Todo{ID: strconv.FormatInt(record.ID, 10), Content: record.Content, Completed: record.Completed, Description: record.Description, DueOn: record.DueOn, StartsOn: record.StartsOn}
	if record.Assignees != nil {
		value.AssigneeIDs = make([]string, 0, len(record.Assignees))
		for _, assignee := range record.Assignees {
			if assignee.ID <= 0 {
				return Todo{}, fmt.Errorf("to-do assignee is invalid")
			}
			value.AssigneeIDs = append(value.AssigneeIDs, strconv.FormatInt(assignee.ID, 10))
		}
	}
	if record.CompletionSubscribers != nil {
		value.CompletionSubscriberIDs = make([]string, 0, len(record.CompletionSubscribers))
		for _, subscriber := range record.CompletionSubscribers {
			if subscriber.ID <= 0 {
				return Todo{}, fmt.Errorf("to-do subscriber is invalid")
			}
			value.CompletionSubscriberIDs = append(value.CompletionSubscriberIDs, strconv.FormatInt(subscriber.ID, 10))
		}
	}
	return value, nil
}
