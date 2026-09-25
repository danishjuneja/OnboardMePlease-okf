package contracts

import "embed"

//go:embed *.json
var schemas embed.FS

func Has(name string) bool {
	_, err := schemas.ReadFile(name)
	return err == nil
}

func Read(name string) ([]byte, error) { return schemas.ReadFile(name) }
