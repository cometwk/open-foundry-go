package spi

// Transaction is an ACID unit of work for write operations.
type Transaction interface {
	CreateObject(typ string, properties map[string]any) (OntologyObject, error)
	UpdateObject(typ, id string, properties map[string]any, expectedVersion *int) (OntologyObject, error)
	DeleteObject(typ, id, mode string) error
	CreateLink(typ, fromID, toID string, properties map[string]any) (OntologyLink, error)
	UpdateLink(typ, linkID string, properties map[string]any, expectedVersion *int) (OntologyLink, error)
	DeleteLink(typ, linkID string) error
	Commit() error
	Rollback() error
}
