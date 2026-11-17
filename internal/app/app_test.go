package app

import (
	"context"
	"testing"
)

func TestStartRejectsInvalidRefresh(t *testing.T) {
	if _, err := Start(context.Background(), Config{Refresh: 0, Listen: "127.0.0.1:0"}); err == nil {
		t.Fatal("expected an error for a zero refresh interval")
	}
	if _, err := Start(context.Background(), Config{Refresh: -1, Listen: "127.0.0.1:0"}); err == nil {
		t.Fatal("expected an error for a negative refresh interval")
	}
}
