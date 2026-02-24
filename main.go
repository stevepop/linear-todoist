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

type LinearIssue struct {
    Type   string `json:"type"`
    Action string `json:"action"`
    Data   struct {
        Identifier string `json:"identifier"`
        Title      string `json:"title"`
        URL        string `json:"url"`
        AssigneeID string `json:"assigneeId"`
    } `json:"data"`
    UpdatedFrom struct {
        AssigneeID *string `json:"assigneeId"`
    } `json:"updatedFrom"`
}

type TodoistTask struct {
    Content    string `json:"content"`
    ProjectID  string `json:"project_id"`
    DueString  string `json:"due_string"`
    DueLang    string `json:"due_lang"`
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

    var issue LinearIssue
    if err := json.Unmarshal(body, &issue); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    // Only handle Issue events assigned to you
    if issue.Type != "Issue" || issue.Data.AssigneeID != os.Getenv("LINEAR_USER_ID") {
        w.WriteHeader(http.StatusNoContent)
        return
    }

	// Ignore deletes
	if issue.Action == "remove" {
		w.WriteHeader(http.StatusNoContent)
		return
	}


    // Check if task already exists in Todoist
    if taskExists(issue.Data.Identifier) {
        log.Printf("Task already exists for %s, skipping", issue.Data.Identifier)
        w.WriteHeader(http.StatusNoContent)
        return
    }

	// For updates, only continue if the assignee field actually changed
	if issue.Action == "update" && issue.UpdatedFrom.AssigneeID == nil {
		log.Printf("Assignee didn't change on %s, skipping", issue.Data.Identifier)
		w.WriteHeader(http.StatusNoContent)
		return
	}

    // Create Todoist task
    if err := createTask(issue.Data.Identifier, issue.Data.Title, issue.Data.URL); err != nil {
        log.Printf("Failed to create task: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    log.Printf("Created Todoist task for %s", issue.Data.Identifier)
    w.WriteHeader(http.StatusNoContent)
}

func verifySignature(body []byte, signature string) bool {
    mac := hmac.New(sha256.New, []byte(os.Getenv("LINEAR_WEBHOOK_SECRET")))
    mac.Write(body)
    expected := hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(signature))
}

func taskExists(identifier string) bool {
    url := fmt.Sprintf("https://api.todoist.com/api/v1/tasks?project_id=%s", os.Getenv("TODOIST_PROJECT_ID"))

    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("Authorization", "Bearer "+os.Getenv("TODOIST_API_TOKEN"))

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return false
    }
    defer resp.Body.Close()

    var tasks []struct {
        Content string `json:"content"`
    }
    json.NewDecoder(resp.Body).Decode(&tasks)

    for _, task := range tasks {
        if strings.Contains(task.Content, identifier) {
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