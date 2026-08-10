package spi_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
)

type stubProvider struct {
	spi.UnimplementedStorageProvider
}

func TestUnimplementedSatisfiesStorageProvider(t *testing.T) {
	var _ spi.StorageProvider = stubProvider{}
}

func TestUnimplementedReturnsSentinel(t *testing.T) {
	p := stubProvider{}
	ctx := spi.RequestContext{TenantID: "t1"}

	_, err := p.CreateObject(ctx, "Widget", map[string]any{"name": "x"})
	if !errors.Is(err, spi.ErrUnimplemented) {
		t.Fatalf("CreateObject: want ErrUnimplemented, got %v", err)
	}
	if err == nil || err.Error() == "" || !contains(err.Error(), "CreateObject") {
		t.Fatalf("CreateObject: want method name in message, got %v", err)
	}

	_, err = p.CreateLink(ctx, "Rel", "a", "b", nil)
	if !errors.Is(err, spi.ErrUnimplemented) || !contains(err.Error(), "CreateLink") {
		t.Fatalf("CreateLink: got %v", err)
	}

	_, err = p.QueryObjects(ctx, "Widget", spi.FilterExpression{}, nil)
	if !errors.Is(err, spi.ErrUnimplemented) || !contains(err.Error(), "QueryObjects") {
		t.Fatalf("QueryObjects: got %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
