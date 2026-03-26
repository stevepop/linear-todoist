package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type LinearIssueWebhook struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	Data   struct {
		Identifier string `json:"identifier"`
		Title      string `json:"title"`
		URL        string `json:"url"`
		AssigneeID string `json:"assigneeId"`
		State      struct {
			Name string `json:"name"`
		} `json:"state"`
	} `json:"data"`
	UpdatedFrom struct {
		AssigneeID *string `json:"assigneeId"`
		StateID    *string `json:"stateId"`
	} `json:"updatedFrom"`
}

type LinearCommentWebhook struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	Data   struct {
		UserID string `json:"userId"`
		Issue  struct {
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			URL        string `json:"url"`
			AssigneeID string `json:"assigneeId"`
		} `json:"issue"`
	} `json:"data"`
}

type TodoistTask struct {
	Content     string `json:"content"`
	ProjectID   string `json:"project_id"`
	DueString   string `json:"due_string"`
	DueLang     string `json:"due_lang"`
	Description string `json:"description"`
}

func main() {
	godotenv.Load()

	http.HandleFunc("/webhooks/linear", handleLinearWebhook)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Listening on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleLinearWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Verify Linear signature
	if !verifySignature(body, r.Header.Get("Linear-Signature")) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Peek at the event type to decide how to route
	var peek struct {
		Type   string `json:"type"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal(body, &peek); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	switch peek.Type {
	case "Issue":
		handleIssueEvent(w, body)
	case "Comment":
		handleCommentEvent(w, body)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleIssueEvent(w http.ResponseWriter, body []byte) {
	var issue LinearIssueWebhook
	if err := json.Unmarshal(body, &issue); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Only handle issues assigned to the configured user
	if issue.Data.AssigneeID != os.Getenv("LINEAR_USER_ID") {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Ignore deletes
	if issue.Action == "remove" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// For updates, only continue if the assignee changed or the issue moved to "In Progress"
	if issue.Action == "update" {
		assigneeChanged := issue.UpdatedFrom.AssigneeID != nil
		movedToInProgress := issue.UpdatedFrom.StateID != nil && issue.Data.State.Name == "In Progress"

		if !assigneeChanged && !movedToInProgress {
			log.Printf("No relevant change on %s, skipping", issue.Data.Identifier)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	// Check if task already exists in Todoist (active or completed)
	if taskExists(issue.Data.Identifier) {
		log.Printf("Task already exists for %s, skipping", issue.Data.Identifier)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := createTask(issue.Data.Identifier, issue.Data.Title, issue.Data.URL); err != nil {
		log.Printf("Failed to create task: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("Created Todoist task for %s", issue.Data.Identifier)
	w.WriteHeader(http.StatusNoContent)
}

func handleCommentEvent(w http.ResponseWriter, body []byte) {
	var comment LinearCommentWebhook
	if err := json.Unmarshal(body, &comment); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Only handle new comments
	if comment.Action != "create" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	issue := comment.Data.Issue

	// Only handle issues assigned to the configured user
	if issue.AssigneeID != os.Getenv("LINEAR_USER_ID") {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Ignore comments made by the assignee themselves
	if comment.Data.UserID == os.Getenv("LINEAR_USER_ID") {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Check if task already exists in Todoist (active or completed)
	if taskExists(issue.Identifier) {
		log.Printf("Task already exists for %s, skipping", issue.Identifier)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := createTask(issue.Identifier, issue.Title, issue.URL); err != nil {
		log.Printf("Failed to create task: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("Created Todoist task for %s (triggered by comment)", issue.Identifier)
	w.WriteHeader(http.StatusNoContent)
}

func verifySignature(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(os.Getenv("LINEAR_WEBHOOK_SECRET")))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func taskExists(identifier string) bool {
	projectID := os.Getenv("TODOIST_PROJECT_ID")
	token := os.Getenv("TODOIST_API_TOKEN")

	// Check active tasks
	activeURL := fmt.Sprintf("https://api.todoist.com/api/v1/tasks?project_id=%s", projectID)
	req, _ := http.NewRequest("GET", activeURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var activeTasks []struct {
		Content string `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&activeTasks)
	for _, task := range activeTasks {
		if strings.Contains(task.Content, identifier) {
			return true
		}
	}

	// Check completed tasks
	completedURL := fmt.Sprintf("https://api.todoist.com/sync/v9/items/completed/get_all?project_id=%s", projectID)
	req2, _ := http.NewRequest("GET", completedURL, nil)
	req2.Header.Set("Authorization", "Bearer "+token)

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return false
	}
	defer resp2.Body.Close()

	var completedResp struct {
		Items []struct {
			Content string `json:"content"`
		} `json:"items"`
	}
	json.NewDecoder(resp2.Body).Decode(&completedResp)
	for _, item := range completedResp.Items {
		if strings.Contains(item.Content, identifier) {
			return true
		}
	}

	return false
}

func createTask(identifier, title, url string) error {
	task := TodoistTask{
		Content:     fmt.Sprintf("[%s] %s", identifier, title),
		ProjectID:   os.Getenv("TODOIST_PROJECT_ID"),
		DueString:   "today at 6pm",
		DueLang:     "en",
		Description: url,
	}

	body, _ := json.Marshal(task)

	req, _ := http.NewRequest("POST", "https://api.todoist.com/api/v1/tasks", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("TODOIST_API_TOKEN"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("todoist returned status %d", resp.StatusCode)
	}
	return nil
}
