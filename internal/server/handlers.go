package server

import (
	"encoding/json"
	"log"
	"net/http"

	"claudecodeproxy/internal/claude"
	"claudecodeproxy/internal/converter"
	"claudecodeproxy/internal/media"
	"claudecodeproxy/internal/types"
)

func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// makeCLIHandler returns an http.HandlerFunc that handles Messages API requests
// by shelling out to the Claude CLI via the given runner.
func (s *Server) makeCLIHandler(runner claude.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.MessagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body: "+err.Error())
			return
		}

		if req.Model == "" {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
			return
		}
		if len(req.Messages) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request_error", "messages is required")
			return
		}

		cliModel, err := claude.MapModel(req.Model)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}

		prompt, tempFiles, err := converter.ConvertMessages(req.System, req.Messages)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "api_error", "converting messages: "+err.Error())
			return
		}
		defer media.Cleanup(tempFiles)

		// Determine temp dir for --add-dir flag
		tempDir := ""
		if len(tempFiles) > 0 {
			tempDir = media.TempDir
		}

		if req.Stream {
			handleCLIStreaming(w, r, runner, cliModel, prompt, tempDir)
		} else {
			handleCLINonStreaming(w, r, runner, req.Model, cliModel, prompt, tempDir)
		}
	}
}

func handleCLINonStreaming(w http.ResponseWriter, r *http.Request, runner claude.Runner, apiModel, cliModel, prompt, tempDir string) {
	result, err := runner.Run(r.Context(), cliModel, prompt, tempDir)
	if err != nil {
		log.Printf("CLI error: %v", err)
		writeError(w, http.StatusInternalServerError, "api_error", "claude CLI error: "+err.Error())
		return
	}

	resp := converter.BuildResponse(apiModel, result)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleCLIStreaming(w http.ResponseWriter, r *http.Request, runner claude.Runner, cliModel, prompt, tempDir string) {
	stdout, wait, err := runner.RunStreaming(r.Context(), cliModel, prompt, tempDir)
	if err != nil {
		log.Printf("CLI streaming error: %v", err)
		writeError(w, http.StatusInternalServerError, "api_error", "claude CLI error: "+err.Error())
		return
	}

	if err := converter.StreamResponse(r.Context(), cliModel, stdout, w); err != nil {
		log.Printf("streaming error: %v", err)
	}

	wait()
}

func writeError(w http.ResponseWriter, status int, errType string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(converter.BuildErrorResponse(errType, message))
}
