package eino

import (
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
		msgs = append(msgs, rowsToMessages(hw.Rows)...)
	}

	return msgs, childMsgs, nil
}
