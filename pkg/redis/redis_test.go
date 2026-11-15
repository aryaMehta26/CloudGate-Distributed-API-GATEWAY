package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/aryaMehta26/cloudgate/internal/config"
)

func TestNewRequiresAddress(t *testing.T) {
	for _, addrs := range [][]string{nil, {""}} {
		if _, err := New(config.RedisConfig{Addrs: addrs}); err == nil {
			t.Fatalf("addrs %q: expected error, not a panic or client", addrs)
		}
	}
}

func TestNewPingsServer(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := New(config.RedisConfig{Addrs: []string{mr.Addr()}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	mr.Close()
	if err = c.Ping(context.Background()); err == nil {
		t.Fatal("ping succeeded after server closed")
	}
}
