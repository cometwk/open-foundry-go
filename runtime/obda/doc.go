// Package obda is the SQL-neutral mapping and planning core.
//
// It compiles ODL storage schema, an OBDA mapping document, and
// StorageProvider requests into a dialect-neutral SQL plan. Dialect
// adapters under obda/dialect emit executable SQL. The v1 runtime
// adapter is SQLite; mapping documents are injected at provider
// construction, not via ApplySchema.
package obda
