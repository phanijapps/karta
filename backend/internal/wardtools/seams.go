package wardtools

import (
	"context"
	"encoding/json"
	"fmt"

	"zbot/backend/internal/tools"
)

// Def is the tool-definition shape (aliased from the engine tool
// plane — one vocabulary).
type Def = tools.Def

// Result is one tool execution outcome (aliased from the engine tool
// plane).
type Result = tools.Result

// decodeArgs parses one tool-call argument object.
func decodeArgs(argsJSON string, out any) error {
	if err := json.Unmarshal([]byte(argsJSON), out); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// compile-time: the tools satisfy the engine tool interface.
var (
	_ tools.Tool = UseTool{}
	_ tools.Tool = CreateTool{}
)

// ExecContext exists for symmetry with future file tools.
type ExecContext = context.Context

// compile-time: the file tools satisfy the engine tool interface.
var (
	_ tools.Tool = ReadTool{}
	_ tools.Tool = WriteTool{}
	_ tools.Tool = ListTool{}
)
