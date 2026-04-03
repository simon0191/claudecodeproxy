package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"claudecodeproxy/internal/proxy"
	"claudecodeproxy/internal/server"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	var port int
	var host string
	var maxConcurrent int
	var mode string

	rootCmd := &cobra.Command{
		Use:     "claudecodeproxy",
		Short:   "Anthropic Messages API proxy for Claude CLI",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Env vars override defaults but flags override env
			if !cmd.Flags().Changed("port") {
				if p := os.Getenv("PORT"); p != "" {
					if v, err := strconv.Atoi(p); err == nil {
						port = v
					}
				}
			}
			if !cmd.Flags().Changed("host") {
				if h := os.Getenv("HOST"); h != "" {
					host = h
				}
			}
			if !cmd.Flags().Changed("max-concurrent") {
				if m := os.Getenv("MAX_CONCURRENT"); m != "" {
					if v, err := strconv.Atoi(m); err == nil {
						maxConcurrent = v
					}
				}
			}
			if !cmd.Flags().Changed("mode") {
				if m := os.Getenv("MODE"); m != "" {
					mode = m
				}
			}

			baseURL := os.Getenv("ANTHROPIC_BASE_URL")

			var srv *server.Server
			switch mode {
			case "cli":
				if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
					fmt.Fprintln(os.Stderr, "warning: CLAUDE_CODE_OAUTH_TOKEN is not set")
				}
				srv = server.NewCLI(host, port, maxConcurrent)
			case "passthrough", "augmented":
				auth, err := resolveAuth()
				if err != nil {
					return err
				}
				if mode == "passthrough" {
					srv = server.NewPassthrough(host, port, auth, baseURL)
				} else {
					srv = server.NewAugmented(host, port, auth, baseURL)
				}
			default:
				return fmt.Errorf("unknown mode: %s (valid: cli, passthrough, augmented)", mode)
			}

			return srv.Start(context.Background())
		},
	}

	rootCmd.Flags().IntVarP(&port, "port", "p", 3456, "listen port")
	rootCmd.Flags().StringVar(&host, "host", "127.0.0.1", "bind address")
	rootCmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 10, "max concurrent CLI processes")
	rootCmd.Flags().StringVar(&mode, "mode", "cli", "proxy mode: cli, passthrough, augmented")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// resolveAuth builds AuthConfig from environment variables.
// Prefers CLAUDE_CODE_OAUTH_TOKEN, falls back to ANTHROPIC_API_KEY.
func resolveAuth() (proxy.AuthConfig, error) {
	if token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		return proxy.AuthConfig{OAuthToken: token}, nil
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return proxy.AuthConfig{APIKey: key}, nil
	}
	return proxy.AuthConfig{}, fmt.Errorf("CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY required for passthrough/augmented mode")
}
