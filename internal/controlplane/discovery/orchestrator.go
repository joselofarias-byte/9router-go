package discovery

import (
	"context"
	"database/sql"
	"time"

	"9router/proxy/internal/controlplane/registry"
	"9router/proxy/internal/log"
)

type Orchestrator struct {
	db       *sql.DB
	adapters []Adapter
}

func NewOrchestrator(db *sql.DB, adapters []Adapter) *Orchestrator {
	return &Orchestrator{
		db:       db,
		adapters: adapters,
	}
}

func (o *Orchestrator) Start(ctx context.Context) {
	go func() {
		// Wait 30s before first run
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}

		o.RunSync(ctx)

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				o.RunSync(ctx)
			}
		}
	}()
}

func (o *Orchestrator) RunSync(ctx context.Context) {
	log.Info("orchestrator", "starting discovery sync pass")

	// Create a new proposed registry state copying existing state manually
	currentState := registry.GetActiveState()

	newState := &registry.RegistryState{
		Providers:      make(map[string]*registry.Provider),
		Models:         make(map[string]*registry.Model),
		ProviderModels: make(map[string]map[string]*registry.ProviderModel),
		Accounts:       make(map[string]*registry.Account),
	}

	if currentState != nil {
		for k, v := range currentState.Providers {
			cp := *v
			newState.Providers[k] = &cp
		}
		for k, v := range currentState.Models {
			cp := *v
			newState.Models[k] = &cp
		}
		for k, vMap := range currentState.ProviderModels {
			newState.ProviderModels[k] = make(map[string]*registry.ProviderModel)
			for subK, v := range vMap {
				cp := *v
				newState.ProviderModels[k][subK] = &cp
			}
		}
		for k, v := range currentState.Accounts {
			cp := *v
			newState.Accounts[k] = &cp
		}
	}

	totalDiscovered := 0
	for _, adapter := range o.adapters {
		candidates, err := adapter.Discover(ctx)
		if err != nil {
			log.Warn("orchestrator", "adapter sync failed", "adapter", adapter.SourceID(), "err", err)
			continue
		}

		totalDiscovered += len(candidates)
		for _, c := range candidates {
			// Upsert Provider
			if _, exists := newState.Providers[c.ProviderID]; !exists {
				newState.Providers[c.ProviderID] = &registry.Provider{
					ID:        c.ProviderID,
					Name:      c.ProviderID,
					IsActive:  true,
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				}
			}

			// Upsert Model
			if _, exists := newState.Models[c.ModelID]; !exists {
				newState.Models[c.ModelID] = &registry.Model{
					ID:        c.ModelID,
					Name:      c.ModelID,
					CreatedAt: time.Now().UTC(),
					UpdatedAt: time.Now().UTC(),
				}
			}

			// Upsert ProviderModel
			if _, ok := newState.ProviderModels[c.ProviderID]; !ok {
				newState.ProviderModels[c.ProviderID] = make(map[string]*registry.ProviderModel)
			}

			pm, exists := newState.ProviderModels[c.ProviderID][c.ModelID]
			if !exists {
				pm = &registry.ProviderModel{
					ProviderID:    c.ProviderID,
					ModelID:       c.ModelID,
					UpstreamModel: c.UpstreamModel,
					PricingMode:   c.PricingMode,
					Capabilities:  c.Capabilities,
					IsActive:      true,
					CreatedAt:     time.Now().UTC(),
				}
				newState.ProviderModels[c.ProviderID][c.ModelID] = pm
			}
			pm.UpdatedAt = time.Now().UTC()
		}
	}

	if totalDiscovered > 0 {
		snap, err := registry.CreateSnapshot(o.db, newState, "discovery_sync")
		if err != nil {
			log.Warn("orchestrator", "failed to create snapshot", "err", err)
			return
		}

		err = registry.ActivateSnapshot(o.db, snap.Version)
		if err != nil {
			log.Warn("orchestrator", "failed to activate snapshot", "err", err)
			return
		}
		log.Info("orchestrator", "sync complete, new snapshot activated", "version", snap.Version, "candidates", totalDiscovered)
	} else {
		log.Info("orchestrator", "sync complete, no new candidates found")
	}
}
