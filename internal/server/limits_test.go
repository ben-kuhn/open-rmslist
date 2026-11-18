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

func TestClientKeyIPv6(t *testing.T) {
	a := httptest.NewRequest("GET", "/", nil)
	a.RemoteAddr = "[2001:db8:1234:5678::1]:443"
	b := httptest.NewRequest("GET", "/", nil)
	b.RemoteAddr = "[2001:db8:1234:5678::ffff]:443"
	if clientKey(a) != clientKey(b) {
		t.Fatalf("same /64 differs: %q vs %q", clientKey(a), clientKey(b))
	}
	c := httptest.NewRequest("GET", "/", nil)
	c.RemoteAddr = "[2001:db8:1234:5679::1]:443"
	if clientKey(a) == clientKey(c) {
		t.Fatal("different /64 collided")
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
