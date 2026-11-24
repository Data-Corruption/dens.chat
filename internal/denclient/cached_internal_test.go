package denclient

import "context"

// SyncCacheAgain keeps a den's state as the cache does every half minute
// while it changes, then asks the den what changed and keeps the state
// again, as the cache does each day and after a failure, for the tests of
// package denclient_test.
func SyncCacheAgain(m *Manager, denID string) error {
	c, err := m.find(denID)
	if err != nil {
		return err
	}
	c.saveKept()
	err = c.syncCache(context.Background())
	c.saveKept()
	return err
}
