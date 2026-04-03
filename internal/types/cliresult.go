package types

// CLIResult is the JSON output from `claude -p --output-format json`.
type CLIResult struct {
	Type      string   `json:"type"`
	Subtype   string   `json:"subtype"`
	IsError   bool     `json:"is_error"`
	Result    string   `json:"result"`
	StopReason string  `json:"stop_reason"`
	SessionID string   `json:"session_id"`
	Usage     CLIUsage `json:"usage"`
	CostUSD   float64  `json:"total_cost_usd"`
}

// CLIUsage contains token usage from the CLI output.
type CLIUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}
