package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/openfoundry/runtime/bootstrap"
)

func seed() error {
	if conf == nil {
		return fmt.Errorf("config required")
	}
	b, err := bootstrap.Open(conf)
	if err != nil {
		slog.Error("open failed", "error", err)
		return err
	}
	defer b.DB.Close()

	if err := b.ApplySchema(); err != nil {
		slog.Error("apply schema failed", "error", err)
		return err
	}
	result, err := b.ApplySeeds()
	if err != nil {
		slog.Error("seed failed", "error", err)
		return err
	}

	tenant := bootstrap.SeedContext(conf.SeedTenant).TenantID
	fmt.Printf(
		"Seed: created %d object(s) + %d link(s), skipped %d existing (tenant %q)\n",
		result.CreatedObjects, result.CreatedLinks, result.SkippedObjects, tenant,
	)
	if strings.TrimSpace(conf.SeedTenant) == "" {
		slog.Warn("SEED_TENANT is unset, so seeds were written under the isolated 'system' tenant — API reads in another tenant will NOT see them")
	}
	return nil
}
