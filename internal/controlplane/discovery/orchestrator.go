package discovery

import (
	"context"
	"database/sql"
	"time"

	"9router/proxy/internal/controlplane/registry"
	cpsync "9router/proxy/internal/controlplane/sync"
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
	}

	// Synchronize Accounts from DB metadata
	if o.db != nil {
		rows, err := o.db.Query("SELECT id, provider, isActive, createdAt, updatedAt FROM providerConnections")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, provider, createdAt, updatedAt string
				var isActive int
				if err := rows.Scan(&id, &provider, &isActive, &createdAt, &updatedAt); err == nil {
					// Safe import: strictly IDs and metadata, NEVER credentials.
					acc := &registry.Account{
						ID:         id,
						ProviderID: provider,
						IsActive:   isActive == 1,
					}
					if t, e := time.Parse(time.RFC3339, createdAt); e == nil {
						acc.CreatedAt = t
					}
					if t, e := time.Parse(time.RFC3339, updatedAt); e == nil {
						acc.UpdatedAt = t
					}
					newState.Accounts[id] = acc
				}
			}
		} else {
			log.Warn("orchestrator", "failed to sync accounts from db", "err", err)
			// fallback to current state if DB query fails
			if currentState != nil {
				for k, v := range currentState.Accounts {
					cp := *v
					newState.Accounts[k] = &cp
				}
			}
		}
	} else if currentState != nil {
		for k, v := range currentState.Accounts {
			cp := *v
			newState.Accounts[k] = &cp
		}
	}

	totalDiscovered := 0
	deactivated := 0
	for _, adapter := range o.adapters {
		candidates, err := adapter.Discover(ctx)
		if err != nil {
			// Keep the previous classification when a source cannot be read.
			// A successful empty catalog is different and deactivates below.
			log.Warn("orchestrator", "adapter sync failed", "adapter", adapter.SourceID(), "err", err)
			continue
		}

		totalDiscovered += len(candidates)
		seen := make(map[string]map[string]bool)
		now := time.Now().UTC()
		for _, c := range candidates {
			if c.ProviderID == "" || c.ModelID == "" {
				continue
			}
			if c.SourceID == "" {
				c.SourceID = adapter.SourceID()
			}

			// Upsert Provider
			if _, exists := newState.Providers[c.ProviderID]; !exists {
				newState.Providers[c.ProviderID] = &registry.Provider{
					ID:        c.ProviderID,
					Name:      c.ProviderID,
					IsActive:  true,
					CreatedAt: now,
					UpdatedAt: now,
				}
			}

			// Upsert Model
			if _, exists := newState.Models[c.ModelID]; !exists {
				newState.Models[c.ModelID] = &registry.Model{
					ID:        c.ModelID,
					Name:      c.ModelID,
					CreatedAt: now,
					UpdatedAt: now,
				}
			}

			if _, ok := newState.ProviderModels[c.ProviderID]; !ok {
				newState.ProviderModels[c.ProviderID] = make(map[string]*registry.ProviderModel)
			}
			if _, ok := seen[c.ProviderID]; !ok {
				seen[c.ProviderID] = make(map[string]bool)
			}
			seen[c.ProviderID][c.ModelID] = true

			pm, exists := newState.ProviderModels[c.ProviderID][c.ModelID]
			if !exists {
				pm = &registry.ProviderModel{
					ProviderID: c.ProviderID,
					ModelID:    c.ModelID,
					CreatedAt:  now,
				}
				newState.ProviderModels[c.ProviderID][c.ModelID] = pm
			}
			applyDiscoveredModel(pm, c, now)
		}

		deactivated += deactivateUnseenModels(newState, adapter, seen)
	}

	if totalDiscovered > 0 || deactivated > 0 {
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
		// The snapshot's account copy can be older than the latest connection
		// sync. Re-read providerConnections so a disconnected provider cannot
		// stay in the free pool via the generation fast path.
		if o.db != nil {
			if err := cpsync.RefreshAccountsAfterSnapshot(o.db); err != nil {
				log.Warn("orchestrator", "account resync after snapshot failed", "err", err)
			}
		}
		log.Info("orchestrator", "sync complete, new snapshot activated", "version", snap.Version, "candidates", totalDiscovered, "deactivated", deactivated)
	} else {
		log.Info("orchestrator", "sync complete, no new candidates found")
	}
}

// applyDiscoveredModel writes the latest observation onto a provider model.
// The same source always replaces pricing, so a model that is no longer free
// leaves the free pool. A different source's "unknown" does not erase a
// concrete free, free_tier, or paid classification.
func applyDiscoveredModel(pm *registry.ProviderModel, c Candidate, now time.Time) {
	if c.UpstreamModel != "" {
		pm.UpstreamModel = c.UpstreamModel
	}
	if c.Capabilities != "" {
		pm.Capabilities = c.Capabilities
	}
	if shouldReplacePricing(pm, c) {
		pm.PricingMode = c.PricingMode
		if pm.PricingMode == "" {
			pm.PricingMode = "unknown"
		}
		pm.SourceID = c.SourceID
	}
	pm.IsActive = true
	pm.UpdatedAt = now
}

func shouldReplacePricing(pm *registry.ProviderModel, c Candidate) bool {
	if pm.SourceID == "" || pm.SourceID == c.SourceID || c.SourceID == "" {
		return true
	}
	if c.PricingMode == "" || c.PricingMode == "unknown" {
		return pm.PricingMode == "" || pm.PricingMode == "unknown"
	}
	return true
}

func deactivateUnseenModels(state *registry.RegistryState, adapter Adapter, seen map[string]map[string]bool) int {
	scoped := map[string]struct{}{}
	for provID := range seen {
		scoped[provID] = struct{}{}
	}
	if scoper, ok := adapter.(ProviderScoper); ok {
		for _, provID := range scoper.ScopedProviderIDs() {
			if provID != "" {
				scoped[provID] = struct{}{}
			}
		}
	}

	deactivated := 0
	for provID := range scoped {
		models := state.ProviderModels[provID]
		for modelID, pm := range models {
			if pm == nil || !pm.IsActive {
				continue
			}
			if seen[provID][modelID] {
				continue
			}
			if !ownedByAdapter(pm, adapter.SourceID()) {
				continue
			}
			pm.IsActive = false
			pm.UpdatedAt = time.Now().UTC()
			deactivated++
		}
	}
	if deactivated > 0 {
		log.Info("orchestrator", "deactivated models missing from current catalog", "adapter", adapter.SourceID(), "count", deactivated)
	}
	return deactivated
}

func ownedByAdapter(pm *registry.ProviderModel, sourceID string) bool {
	return pm.SourceID == "" || pm.SourceID == sourceID || pm.SourceID == "models.dev"
}
