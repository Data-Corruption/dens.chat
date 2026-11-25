package denclient

// ForgetSignedKeys drops the signed keys a Dens holds for a den, as one
// whose socket hasn't brought them yet doesn't hold them, for the tests of
// package denclient_test.
func ForgetSignedKeys(m *Manager, denID string) {
	c, err := m.find(denID)
	if err != nil {
		panic(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.den.signed)
	clear(c.verdicts)
}
