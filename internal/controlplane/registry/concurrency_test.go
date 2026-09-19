package registry

import (
	"sync"
	"testing"
)

func TestUpdateAccounts_Concurrency(t *testing.T) {
	InitRegistry(nil)

	// Pre-populate some base state
	stateMu.Lock()
	activeState.Providers["prov"] = &Provider{ID: "prov"}
	stateMu.Unlock()

	var wg sync.WaitGroup

	// Writer continuously swaps accounts
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			accs := map[string]*Account{
				"acc1": {ID: "acc1"},
			}
			UpdateAccounts(accs)
		}
	}()

	// Readers continuously read the active state while iterators run
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				st := GetActiveState()
				if st != nil {
					// Iterating over the map while it might be swapped out
					// If COW is broken and it edits the same map, this will panic with concurrent map read/write
					for k, v := range st.Accounts {
						_ = k
						_ = v.ID
					}
				}
			}
		}()
	}

	wg.Wait()
}
