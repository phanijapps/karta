package chitta

// The fake driver implementation lives in fakedriver.go (production
// file) so OpenInMemory can share it with the distiller package's
// tests. Fake-driver BEHAVIOR tests live here.

import (
	"context"
	"testing"

	"zbot/backend/internal/domain"
)

func TestFakeDriverCursorStability(t *testing.T) {
	f := newFakeDriver()
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		_ = f.PutRecordForTest(ctx, i)
	}
	p1, _ := f.ListRecords(ctx, domain.HistoryRequest{Limit: 3})
	p2, _ := f.ListRecords(ctx, domain.HistoryRequest{Limit: 3})
	if len(p1.Items) != len(p2.Items) {
		t.Fatalf("identical inputs must give identical pages")
	}
}
