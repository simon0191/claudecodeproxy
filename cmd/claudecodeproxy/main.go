package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"claudecodeproxy/internal/server"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	var port int
	var host string
	var maxConcurrent int

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

			if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
				fmt.Fprintln(os.Stderr, "warning: CLAUDE_CODE_OAUTH_TOKEN is not set")
			}

			srv := server.New(host, port, maxConcurrent)
			return srv.Start(context.Background())
		},
	}

	rootCmd.Flags().IntVarP(&port, "port", "p", 3456, "listen port")
	rootCmd.Flags().StringVar(&host, "host", "127.0.0.1", "bind address")
	rootCmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 10, "max concurrent CLI processes")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
