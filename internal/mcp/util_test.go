package mcp

import "os"

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }
func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}
