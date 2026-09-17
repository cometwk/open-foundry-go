// Copyright 2016 The Xorm Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// WK: handle `ilike` for postgresql
package mybuilder

import (
	"fmt"

	"xorm.io/builder"
)

// ILike defines like condition
type ILike [2]string

var _ builder.Cond = ILike{"", ""}

// WriteTo write SQL to Writer
func (like ILike) WriteTo(w builder.Writer) error {
	if _, err := fmt.Fprintf(w, "%s ILIKE ?", like[0]); err != nil {
		return err
	}
	// FIXME: if use other regular express, this will be failed. but for compatible, keep this
	if like[1][0] == '%' || like[1][len(like[1])-1] == '%' {
		w.Append(like[1])
	} else {
		w.Append("%" + like[1] + "%")
	}
	return nil
}

// And implements And with other conditions
func (like ILike) And(conds ...builder.Cond) builder.Cond {
	return builder.And(like, builder.And(conds...))
}

// Or implements Or with other conditions
func (like ILike) Or(conds ...builder.Cond) builder.Cond {
	return builder.Or(like, builder.Or(conds...))
}

// IsValid tests if this condition is valid
func (like ILike) IsValid() bool {
	return len(like[0]) > 0 && len(like[1]) > 0
}
