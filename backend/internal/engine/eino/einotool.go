// The tool adapter (engine-adk-spine): the engine's tools.Tool on the
// ADK's tool interface. Thin on purpose — an Exec error is returned as
// the endpoint error; the persistence middleware's wrapper turns it into
// the model-facing "error: …" result (the legacy loop's posture) and
// records it with the progress tracker.

package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/tools"
)

type einoTool struct{ t tools.Tool }

func (e einoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	d := e.t.Def()
	return &schema.ToolInfo{Name: d.Name, Desc: d.Description, ParamsOneOf: paramsOneOfFromJSON(d.ParamsJSON)}, nil
}

func (e einoTool) InvokableRun(ctx context.Context, argsJSON string, _ ...tool.Option) (string, error) {
	res := e.t.Exec(ctx, argsJSON)
	if res.Err != nil {
		return "", res.Err
	}
	return res.Output, nil
}

// einoTools adapts a run's inventory for the ADK ToolsNode.
func einoTools(ts []tools.Tool) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(ts))
	for _, t := range ts {
		out = append(out, einoTool{t: t})
	}
	return out
}

// unknownToolResult is the model-facing result for a tool name outside
// this run's inventory (a hallucinated call): the typed error the
// legacy loop's execOffered returns, as text — never execution.
func unknownToolResult(name string) string {
	return "error: " + fmt.Errorf("%w: %q", ErrToolNotOffered, name).Error()
}

// placeholderTool keeps a TOOL-LESS run on the ADK's ReAct path. With no
// tool configured the ADK degrades to a single model call (no tool node),
// so a hallucinated call would become the run's "answer"; the legacy loop
// refuses such calls until the model answers. The placeholder is never
// offered (the budget middleware empties state.ToolInfos on a tool-less
// run — zero schema tokens) and never executes (every wrapper refuses it).
type placeholderTool struct{}

const placeholderToolName = "__karta_placeholder"

func (placeholderTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: placeholderToolName, Desc: "never offered; keeps a tool-less run on the ReAct path",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}

func (placeholderTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "", ErrToolNotOffered
}
