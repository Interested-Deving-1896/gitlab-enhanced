package storage

import "fmt"

func errNotImplemented(method string) error {
	return fmt.Errorf("%s: not yet implemented", method)
}
