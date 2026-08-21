package sqlite

import (
	"errors"
	"strings"

	"github.com/openfoundry/runtime/spi"
)

// Classify maps a SQLite driver error onto an SPI sentinel when possible.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unique constraint"):
		if strings.Contains(msg, "version") {
			return fmtWrap(spi.ErrVersionConflict, err)
		}
		return fmtWrap(spi.ErrCardinalityViolation, err)
	case strings.Contains(msg, "no such table"):
		return fmtWrap(spi.ErrSourceSchemaDrift, err)
	default:
		return err
	}
}

func fmtWrap(sent, err error) error {
	return errors.Join(sent, err)
}
