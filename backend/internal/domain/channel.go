package domain

import "context"

// Channel is the Day-2 extension seam for messaging plugins (WhatsApp,
// Signal, Slack, ...). Implementations live in internal/channels/<name>/ and
// are wired by configuration; the core never imports a channel package.
type Channel interface {
	Name() string
	Start(ctx context.Context, bus EventBus) error
	Stop(ctx context.Context) error
}
