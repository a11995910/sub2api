package service

import "time"

const (
	CreativeDrawingTaskStatusQueued  = "queued"
	CreativeDrawingTaskStatusRunning = "running"
	CreativeDrawingTaskStatusSuccess = "success"
	CreativeDrawingTaskStatusError   = "error"

	CreativeDrawingModeGenerate = "generate"
	CreativeDrawingModeEdit     = "edit"
)

type CreativeDrawingReference struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	DataURL   string `json:"data_url"`
	RemoteURL string `json:"remote_url,omitempty"`
	Source    string `json:"source"`
}

type CreativeDrawingImageResult struct {
	ID            string `json:"id"`
	URL           string `json:"url,omitempty"`
	SourceURL     string `json:"source_url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
	OutputFormat  string `json:"output_format,omitempty"`
	Size          string `json:"size,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
}

type CreativeDrawingTask struct {
	ID              string                       `json:"id"`
	UserID          int64                        `json:"user_id"`
	APIKeyID        int64                        `json:"api_key_id"`
	ConversationID  string                       `json:"conversation_id"`
	TurnID          string                       `json:"turn_id"`
	Mode            string                       `json:"mode"`
	Model           string                       `json:"model"`
	Prompt          string                       `json:"prompt"`
	Size            string                       `json:"size,omitempty"`
	Count           int                          `json:"count"`
	OutputFormat    string                       `json:"output_format"`
	ReferenceImages []CreativeDrawingReference   `json:"reference_images"`
	Status          string                       `json:"status"`
	Error           string                       `json:"error,omitempty"`
	Images          []CreativeDrawingImageResult `json:"images"`
	RequestJSON     map[string]any               `json:"-"`
	CreatedAt       time.Time                    `json:"created_at"`
	UpdatedAt       time.Time                    `json:"updated_at"`
	StartedAt       *time.Time                   `json:"started_at,omitempty"`
	CompletedAt     *time.Time                   `json:"completed_at,omitempty"`
}

type CreativeDrawingCreateTaskRequest struct {
	APIKeyID        int64                      `json:"api_key_id"`
	ConversationID  string                     `json:"conversation_id"`
	TurnID          string                     `json:"turn_id"`
	Mode            string                     `json:"mode"`
	Model           string                     `json:"model"`
	Prompt          string                     `json:"prompt"`
	Size            string                     `json:"size"`
	Count           int                        `json:"count"`
	OutputFormat    string                     `json:"output_format"`
	ReferenceImages []CreativeDrawingReference `json:"reference_images"`
}
