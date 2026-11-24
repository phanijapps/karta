package main

// The retrieval plane's daemon wiring (ward-v2): the MCP executor over
// the registry's enabled stdio servers, refreshed on boot and on every
// registry change. Extracted from main.go (LOC budget).

import (
	"context"
	"log"

	einoengine "zbot/backend/internal/engine/eino"

	"zbot/backend/internal/mcpclient"
	"zbot/backend/internal/registry"
)

// wireRetrievalPlane connects the executor to the engine (root runs
// mount live MCP tools) and returns the refresh hook for the registry
// watcher.
func wireRetrievalPlane(ctx context.Context, eng *einoengine.Adapter, snapshot func() registry.Snapshot) *mcpclient.Executor {
	mcpExec := &mcpclient.Executor{
		Specs: func() []mcpclient.ServerSpec {
			var out []mcpclient.ServerSpec
			for name, spec := range snapshot().MCPs {
				if spec.Disabled || spec.Command == "" {
					continue // http transports + disabled servers stay off v1
				}
				out = append(out, mcpclient.ServerSpec{Name: name, Spec: spec})
			}
			return out
		},
		Log: func(format string, args ...any) { log.Printf("zbotd: "+format, args...) },
	}
	mcpExec.Refresh(ctx)
	eng.SetMCPTools(mcpExec.Tools)
	return mcpExec
}
