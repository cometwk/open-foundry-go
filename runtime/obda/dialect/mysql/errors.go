package mysql

import (
	"errors"
	"strings"

	"github.com/openfoundry/runtime/spi"
)

// Classify maps a go-sql-driver error onto an SPI sentinel when possible.
// The driver formats server errors as "Error <number>: <message>", so the
// stable server error numbers are matched textually and no driver types
// are imported: 1062 duplicate entry, 1146 no such table,
// 1049 unknown database.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Error 1062"):
		if strings.Contains(strings.ToLower(msg), "version") {
			return errors.Join(spi.ErrVersionConflict, err)
		}
		return errors.Join(spi.ErrCardinalityViolation, err)
	case strings.Contains(msg, "Error 1146"), strings.Contains(msg, "Error 1049"):
		return errors.Join(spi.ErrSourceSchemaDrift, err)
	default:
		return err
	}
}
