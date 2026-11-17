package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newIPLimiter(1, 3)
	for i := 0; i < 3; i++ {
		if !l.allow("a", now) {
			t.Fatalf("request %d rejected despite burst", i)
		}
	}
	if l.allow("a", now) {
		t.Fatal("burst limit not enforced")
	}
	if !l.allow("b", now) {
		t.Fatal("limit leaked to another client")
	}
	if !l.allow("a", now.Add(time.Second)) {
		t.Fatal("bucket did not refill")
	}
}

func TestClientGate(t *testing.T) {
	g := newClientGate()
	if !g.acquire("a") {
		t.Fatal("first acquire failed")
	}
	if g.acquire("a") {
		t.Fatal("second acquire for the same client succeeded")
	}
	if !g.acquire("b") {
		t.Fatal("another client was blocked")
	}
	g.release("a")
	if !g.acquire("a") {
		t.Fatal("release did not free the client")
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 198.51.100.7")
	if got := clientIP(req); got != "198.51.100.7" {
		t.Fatalf("proxied client = %q", got)
	}
	req.RemoteAddr = "192.0.2.9:1234"
	if got := clientIP(req); got != "192.0.2.9" {
		t.Fatalf("direct client = %q", got)
	}
}
