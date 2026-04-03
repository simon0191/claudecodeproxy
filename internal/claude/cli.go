package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"claudecodeproxy/internal/types"
)

// Runner abstracts CLI invocation so it can be replaced in tests.
type Runner interface {
	Run(ctx context.Context, model string, prompt string, tempDir string) (*types.CLIResult, error)
	RunStreaming(ctx context.Context, model string, prompt string, tempDir string) (io.ReadCloser, WaitFunc, error)
}

// WaitFunc waits for the CLI process to finish.
type WaitFunc func() error

// CLIRunner invokes the real Claude CLI with concurrency limiting.
type CLIRunner struct {
	sem chan struct{}
}

// NewCLIRunner creates a CLIRunner that allows at most maxConcurrent CLI processes.
func NewCLIRunner(maxConcurrent int) *CLIRunner {
	return &CLIRunner{sem: make(chan struct{}, maxConcurrent)}
}

func (c *CLIRunner) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("request cancelled while waiting for available CLI slot: %w", ctx.Err())
	}
}

func (c *CLIRunner) release() {
	<-c.sem
}

func (c *CLIRunner) Run(ctx context.Context, model string, prompt string, tempDir string) (*types.CLIResult, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	cmd := buildCommand(ctx, model, "json", false, tempDir)
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude CLI: %w: %s", err, stderr.String())
	}

	var result types.CLIResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("parsing CLI output: %w", err)
	}

	if result.IsError {
		return nil, fmt.Errorf("claude CLI error: %s", result.Result)
	}

	return &result, nil
}

func (c *CLIRunner) RunStreaming(ctx context.Context, model string, prompt string, tempDir string) (io.ReadCloser, WaitFunc, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, nil, err
	}

	cmd := buildCommand(ctx, model, "stream-json", true, tempDir)
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.release()
		return nil, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		c.release()
		return nil, nil, fmt.Errorf("starting claude CLI: %w", err)
	}

	// Release the semaphore slot when the caller finishes with the process.
	wait := func() error {
		defer c.release()
		return cmd.Wait()
	}

	return stdout, wait, nil
}

func buildCommand(ctx context.Context, model string, outputFormat string, streaming bool, tempDir string) *exec.Cmd {
	args := []string{
		"-p",
		"--output-format", outputFormat,
		"--model", model,
		"--dangerously-skip-permissions",
	}
	if streaming {
		args = append(args, "--verbose", "--include-partial-messages")
	}
	if tempDir != "" {
		args = append(args, "--add-dir", tempDir)
	}
	return exec.CommandContext(ctx, "claude", args...)
}
