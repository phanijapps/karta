package eino

import (
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// buildMessages assembles the model's message list for one step. Root:
// compaction-aware session history. Child: a FRESH context — system
// prompt + the delegated task only (the parent embeds needed context
// in the task; inheriting the whole session transcript pollutes the
// child and bills the parent's history twice).
func (a *Adapter) buildMessages(in runInput, childMsgs []*schema.Message) ([]*schema.Message, []*schema.Message, error) {
	var msgs []*schema.Message
	if in.isChild && childMsgs != nil {
		msgs = childMsgs // retained child conversation (tool rounds feed back)
	} else {
		msgs = append(msgs, schema.SystemMessage(in.systemPrompt))
	}
	if in.isChild {
		if childMsgs == nil {
			msgs = append(msgs, schema.UserMessage(in.goal))
			childMsgs = msgs // fresh child context (first step)
		}
	} else {
		hw, err := a.st.RootWindowFull(in.sessionID, in.execID, 200)
		if err != nil {
			return nil, nil, fmt.Errorf("engine: history: %w", err)
		}
		if hw.Summary != nil {
			msgs = append(msgs, schema.SystemMessage(hw.Summary.Content))
		}
		for _, m := range hw.Rows {
			switch m.Role {
			case "user":
				msgs = append(msgs, schema.UserMessage(m.Content))
			case "assistant":
				am := &schema.Message{Role: schema.Assistant, Content: m.Content}
				if m.ToolCalls != "" {
					_ = json.Unmarshal([]byte(m.ToolCalls), &am.ToolCalls)
				}
				msgs = append(msgs, am)
			case "tool":
				msgs = append(msgs, &schema.Message{Role: schema.Tool, Content: m.Content, ToolCallID: m.ToolCallID, ToolName: m.ToolName})
			}
		}
	}

	return msgs, childMsgs, nil
}
