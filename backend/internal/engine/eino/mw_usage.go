// The usage middleware (engine-adk-spine): WrapModel around every model
// call — per-call usage onto the run totals (the legacy loop read it off
// each completion). Slice 1 adds the per-agent reasoning effort option
// here: this is the one seam where a call's options are composed.

package eino

import (
	"context"
	"errors"
	"io"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type usageMW struct {
	adk.BaseChatModelAgentMiddleware
	st *runState
}

func (m *usageMW) WrapModel(_ context.Context, inner model.BaseModel[*schema.Message], _ *adk.ModelContext) (model.BaseModel[*schema.Message], error) {
	return &usageModel{BaseModel: inner, st: m.st}, nil
}

type usageModel struct {
	model.BaseModel[*schema.Message]
	st *runState
}

func (u *usageModel) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	out, err := u.BaseModel.Generate(ctx, msgs, opts...)
	if err == nil {
		u.st.addUsage(out.ResponseMeta)
	}
	return out, err
}

// Stream passes the stream through and books the LAST usage the
// provider reports (OpenAI-compatible servers send it once, on the
// final chunk) — runs are non-streaming today, so this is correctness
// insurance for the day they are not.
func (u *usageModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, err := u.BaseModel.Stream(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	pr, pw := schema.Pipe[*schema.Message](1)
	go func() {
		defer pw.Close()
		var last *schema.ResponseMeta
		for {
			msg, err := sr.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					pw.Send(nil, err)
				}
				if last != nil {
					u.st.addUsage(last)
				}
				return
			}
			if msg.ResponseMeta != nil && msg.ResponseMeta.Usage != nil {
				last = msg.ResponseMeta
			}
			pw.Send(msg, nil)
		}
	}()
	return pr, nil
}
