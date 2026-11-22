package ladybug

import (
	"context"
	"testing"
)

func TestTxProbe(t *testing.T) {
	parent := t.TempDir()
	d, err := Open(parent, 0)
	if err != nil {
		t.Skipf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	must := func(err error) {
		if err != nil {
			t.Fatalf("tx probe: %v", err)
		}
	}
	// Manual: BEGIN, write, ROLLBACK, verify absence; then BEGIN, write, COMMIT.
	if err := d.exec(context.Background(), "BEGIN TRANSACTION", nil); err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	must(d.exec(context.Background(), `CREATE (m:Memory {id: 'tx_probe', kind: 'fact', content: 'x', normalized_content: 'x', confidence: 0.5, subject: 'user', status: 'active', mention_count: 1, created_at: timestamp('2026-11-21 12:00:00'), updated_at: timestamp('2026-11-21 12:00:00')})`, nil))
	if err := d.exec(context.Background(), "ROLLBACK", nil); err != nil {
		t.Fatalf("ROLLBACK: %v", err)
	}
	rows, err := d.queryRows(context.Background(), "MATCH (m:Memory {id: 'tx_probe'}) RETURN m.id AS id", nil)
	must(err)
	if len(rows) != 0 {
		t.Fatal("rollback must discard the write")
	}
	if err := d.exec(context.Background(), "BEGIN TRANSACTION", nil); err != nil {
		t.Fatalf("BEGIN2: %v", err)
	}
	must(d.exec(context.Background(), `CREATE (m:Memory {id: 'tx_probe2', kind: 'fact', content: 'x', normalized_content: 'x', confidence: 0.5, subject: 'user', status: 'active', mention_count: 1, created_at: timestamp('2026-11-21 12:00:00'), updated_at: timestamp('2026-11-21 12:00:00')})`, nil))
	if err := d.exec(context.Background(), "COMMIT", nil); err != nil {
		t.Fatalf("COMMIT: %v", err)
	}
	rows, err = d.queryRows(context.Background(), "MATCH (m:Memory {id: 'tx_probe2'}) RETURN m.id AS id", nil)
	must(err)
	if len(rows) != 1 {
		t.Fatal("commit must persist the write")
	}
}
