package pg

import (
	"context"
	"errors"
	"testing"

	"github.com/ethchor/phenk/internal/core"
)

func TestDomainCRUD(t *testing.T) {
	reset(t)
	ctx := context.Background()

	d := &core.Domain{Name: "phenk.test", State: core.DomainActive, Pool: core.PoolRandom}
	if err := CreateDomain(ctx, testDB, d); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if d.ID.IsZero() || d.CreatedAt.IsZero() {
		t.Fatal("CreateDomain did not fill in the generated fields")
	}

	byName, err := DomainByName(ctx, testDB, "phenk.test")
	if err != nil {
		t.Fatalf("DomainByName: %v", err)
	}
	if byName.ID != d.ID || byName.Pool != core.PoolRandom {
		t.Fatalf("DomainByName returned %+v", byName)
	}

	byID, err := DomainByID(ctx, testDB, d.ID)
	if err != nil || byID.Name != "phenk.test" {
		t.Fatalf("DomainByID: %+v %v", byID, err)
	}

	if _, err := DomainByName(ctx, testDB, "nope.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	dup := &core.Domain{Name: "phenk.test", State: core.DomainFresh, Pool: core.PoolPublic}
	if err := CreateDomain(ctx, testDB, dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate domain: got %v, want ErrConflict", err)
	}
}

func TestAllocatableDomainsExcludesEverythingButActive(t *testing.T) {
	reset(t)
	ctx := context.Background()

	for _, d := range []*core.Domain{
		{Name: "active-random.test", State: core.DomainActive, Pool: core.PoolRandom},
		{Name: "fresh-random.test", State: core.DomainFresh, Pool: core.PoolRandom},
		{Name: "burned-random.test", State: core.DomainBurned, Pool: core.PoolRandom},
		{Name: "retired-random.test", State: core.DomainRetired, Pool: core.PoolRandom},
		{Name: "active-public.test", State: core.DomainActive, Pool: core.PoolPublic},
	} {
		if err := CreateDomain(ctx, testDB, d); err != nil {
			t.Fatalf("CreateDomain(%s): %v", d.Name, err)
		}
	}

	random, err := AllocatableDomains(ctx, testDB, core.PoolRandom)
	if err != nil {
		t.Fatalf("AllocatableDomains: %v", err)
	}
	if len(random) != 1 || random[0].Name != "active-random.test" {
		t.Fatalf("random pool = %+v, want only the active domain", random)
	}

	// The pools never mix.
	public, err := AllocatableDomains(ctx, testDB, core.PoolPublic)
	if err != nil {
		t.Fatalf("AllocatableDomains: %v", err)
	}
	if len(public) != 1 || public[0].Name != "active-public.test" {
		t.Fatalf("public pool = %+v", public)
	}
}

func TestSetDomainStateStampsBurnedAt(t *testing.T) {
	reset(t)
	ctx := context.Background()

	d := &core.Domain{Name: "burnme.test", State: core.DomainActive, Pool: core.PoolPublic}
	if err := CreateDomain(ctx, testDB, d); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if err := SetDomainState(ctx, testDB, d.ID, core.DomainBurned); err != nil {
		t.Fatalf("SetDomainState: %v", err)
	}
	got, err := DomainByID(ctx, testDB, d.ID)
	if err != nil {
		t.Fatalf("DomainByID: %v", err)
	}
	if got.State != core.DomainBurned || got.BurnedAt == nil {
		t.Fatalf("got %+v, want burned with a burned_at stamp", got)
	}

	first := *got.BurnedAt
	if err := SetDomainState(ctx, testDB, d.ID, core.DomainBurned); err != nil {
		t.Fatalf("repeat SetDomainState: %v", err)
	}
	got, _ = DomainByID(ctx, testDB, d.ID)
	if !got.BurnedAt.Equal(first) {
		t.Fatal("re-burning a domain moved burned_at")
	}

	if err := SetDomainState(ctx, testDB, core.NewUUID(), core.DomainActive); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
