//go:build windows

package state

// pidAlive: no Windows não há checagem barata e segura do pid aqui; vale só a idade do lock.
func pidAlive(int) bool { return false }
