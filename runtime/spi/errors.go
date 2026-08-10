package spi

import "errors"

// ErrUnimplemented is returned by Phase 1 stubs for non-schema SPI methods.
var ErrUnimplemented = errors.New("openfoundry: unimplemented")
