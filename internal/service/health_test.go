package service

import (
	"context"
	"testing"

	"github.com/harlew-dev/lewbox/internal/db"
)

func TestHealthReportsReady(t *testing.T) {
	s := newTestService(t)
	resp, err := s.Health(context.Background())
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if !resp.OK {
		t.Errorf("health = %+v, want ok", resp)
	}
}

func TestHealthFailsWhenDatabaseIsClosed(t *testing.T) {
	conn, err := db.Open(context.Background(), t.TempDir()+"/lewbox.db")
	if err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	resp, err := s.Health(context.Background())
	if resp.OK {
		t.Errorf("health = %+v with the database closed", resp)
	}
	se := wantCode(t, err, CodeUnavailable)
	if se.Message != "database not reachable" {
		t.Errorf("message = %q; a public route must not expose the database error", se.Message)
	}
}
