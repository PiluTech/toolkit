package resume

// ReadOwnExecutableForTest expõe readOwnExecutable ao pacote de testes externo.
func ReadOwnExecutableForTest(p string) ([]byte, error) { return readOwnExecutable(p) }
