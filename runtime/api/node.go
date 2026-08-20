package api

import (
	"context"
	"errors"
	"fmt"

	graphql "github.com/graph-gophers/graphql-go"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/query"
	"github.com/openfoundry/runtime/spi"
)

// node is the GraphQL object resolver for every ObjectType. Field methods
// are a union of supply-chain (and engine test fixture) names; extra
// methods are unused. Query root fields are bound dynamically from IR.
type node struct {
	srv *Server
	typ string
	obj spi.OntologyObject
}

func (s *Server) wrap(typ string, obj spi.OntologyObject) *node {
	if obj == nil {
		return nil
	}
	return &node{srv: s, typ: typ, obj: obj}
}

func (n *node) idString() string {
	s, _ := n.obj[spi.FieldID].(string)
	return s
}

func (n *node) Id() graphql.ID { return graphql.ID(n.idString()) }

func (n *node) Sku() string                  { return n.str("sku") }
func (n *node) Name() string                 { return n.str("name") }
func (n *node) Category() string             { return n.str("category") }
func (n *node) UnitOfMeasure() string        { return n.str("unitOfMeasure") }
func (n *node) ReorderPoint() int32          { return n.i32("reorderPoint") }
func (n *node) ReorderQuantity() int32       { return n.i32("reorderQuantity") }
func (n *node) Code() string                 { return n.str("code") }
func (n *node) Tier() string                 { return n.str("tier") }
func (n *node) ContactName() *string         { return n.strPtr("contactName") }
func (n *node) ContactEmail() *string        { return n.strPtr("contactEmail") }
func (n *node) Country() string              { return n.str("country") }
func (n *node) LeadTimeDays() *int32         { return n.i32Ptr("leadTimeDays") }
func (n *node) OnTimeDeliveryRate() *float64 { return n.f64Ptr("onTimeDeliveryRate") }
func (n *node) Type() string                 { return n.str("type") }
func (n *node) Status() string               { return n.str("status") }
func (n *node) Address() *string             { return n.strPtr("address") }
func (n *node) Capacity() int32              { return n.i32("capacity") }
func (n *node) OrderNumber() string          { return n.str("orderNumber") }
func (n *node) Quantity() int32              { return n.i32("quantity") }
func (n *node) UnitCost() float64            { return n.f64("unitCost") }
func (n *node) Currency() string             { return n.str("currency") }
func (n *node) RequestedDeliveryDate() DateTime {
	return DateTime(n.str("requestedDeliveryDate"))
}
func (n *node) Notes() *string           { return n.strPtr("notes") }
func (n *node) TrackingNumber() *string  { return n.strPtr("trackingNumber") }
func (n *node) TransportMode() string    { return n.str("transportMode") }
func (n *node) DepartureDate() *DateTime { return n.dtPtr("departureDate") }
func (n *node) EstimatedArrival() *DateTime {
	return n.dtPtr("estimatedArrival")
}
func (n *node) ActualArrival() *DateTime { return n.dtPtr("actualArrival") }
func (n *node) ReservedQuantity() int32  { return n.i32("reservedQuantity") }
func (n *node) StockLevel() string       { return n.str("stockLevel") }
func (n *node) LastCountDate() *DateTime { return n.dtPtr("lastCountDate") }
func (n *node) Qty() *int32              { return n.i32Ptr("qty") }

func (n *node) Suppliers(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "suppliers")
}
func (n *node) Products(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "products")
}
func (n *node) CurrentShipments(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "currentShipments")
}
func (n *node) InventoryRecords(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "inventoryRecords")
}
func (n *node) TrackedProduct(ctx context.Context) (*node, error) {
	list, err := n.srv.resolveLink(ctx, n, "trackedProduct")
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}
func (n *node) B(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "b")
}
func (n *node) C(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "c")
}
func (n *node) D(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "d")
}
func (n *node) Leaf(ctx context.Context) ([]*node, error) {
	return n.srv.resolveLink(ctx, n, "leaf")
}

func (n *node) Supplier(ctx context.Context) (*node, error) {
	return n.srv.resolveFK(ctx, n, "supplier")
}
func (n *node) Product(ctx context.Context) (*node, error) {
	return n.srv.resolveFK(ctx, n, "product")
}
func (n *node) Facility(ctx context.Context) (*node, error) {
	return n.srv.resolveFK(ctx, n, "facility")
}
func (n *node) Order(ctx context.Context) (*node, error) {
	return n.srv.resolveFK(ctx, n, "order")
}
func (n *node) Origin(ctx context.Context) (*node, error) {
	return n.srv.resolveFK(ctx, n, "origin")
}
func (n *node) Destination(ctx context.Context) (*node, error) {
	return n.srv.resolveFK(ctx, n, "destination")
}

func (n *node) CurrentUtilization(ctx context.Context) (*int32, error) {
	return n.computedInt(ctx, "currentUtilization")
}
func (n *node) ActiveOrders(ctx context.Context) (*int32, error) {
	return n.computedInt(ctx, "activeOrders")
}

func (n *node) computedInt(ctx context.Context, field string) (*int32, error) {
	v, err := n.srv.engine.ComputeField(rcFrom(ctx), n.typ, n.idString(), field)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	i, ok := toInt32(v)
	if !ok {
		return nil, fmt.Errorf("computed %s: expected int, got %T", field, v)
	}
	return &i, nil
}

func (s *Server) resolveLink(ctx context.Context, n *node, fieldName string) ([]*node, error) {
	if kids, ok := memoFrom(ctx).lookup(n.idString(), fieldName); ok {
		return s.wrapMany(fieldName, n.typ, kids), nil
	}
	ex, err := compileExpand(ctx, s.engine.Ontology(), n.typ, fieldName)
	if err != nil {
		return []*node{}, nil
	}
	ex.StartID = n.idString()
	res, err := query.Execute(s.engine, rcFrom(ctx), query.Op{Expand: ex})
	if err != nil {
		return nil, err
	}
	if res.Expand != nil {
		memoFrom(ctx).merge(res.Expand.Adjacency)
		return s.wrapMany(fieldName, n.typ, res.Expand.FirstHop), nil
	}
	return []*node{}, nil
}

func (s *Server) wrapMany(fieldName, startType string, objs []spi.OntologyObject) []*node {
	target := startType
	if ot := s.engine.Ontology().ObjectByName(startType); ot != nil {
		if f := fieldByName(ot, fieldName); f != nil {
			target = f.Type.Name
		}
	}
	out := make([]*node, 0, len(objs))
	for _, obj := range objs {
		typ := target
		if t, _ := obj[spi.FieldType].(string); t != "" {
			typ = t
		}
		out = append(out, s.wrap(typ, obj))
	}
	if out == nil {
		out = []*node{}
	}
	return out
}

func (s *Server) resolveFK(ctx context.Context, n *node, fieldName string) (*node, error) {
	ot := s.engine.Ontology().ObjectByName(n.typ)
	if ot == nil {
		return nil, nil
	}
	f := fieldByName(ot, fieldName)
	if f == nil {
		return nil, nil
	}
	raw := n.obj[fieldName]
	id, _ := raw.(string)
	if id == "" {
		return nil, nil
	}
	res, err := query.Execute(s.engine, rcFrom(ctx), query.Op{Get: &query.Get{Type: f.Type.Name, ID: id}})
	if err != nil {
		if errors.Is(err, spi.ErrObjectNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return s.wrap(f.Type.Name, res.Object), nil
}

func fieldByName(ot *ir.ObjectType, name string) *ir.Field {
	for i := range ot.Fields {
		if ot.Fields[i].Name == name {
			return &ot.Fields[i]
		}
	}
	return nil
}

func (n *node) str(field string) string {
	s, _ := n.obj[field].(string)
	return s
}

func (n *node) strPtr(field string) *string {
	v, ok := n.obj[field]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

func (n *node) i32(field string) int32 {
	i, _ := toInt32(n.obj[field])
	return i
}

func (n *node) i32Ptr(field string) *int32 {
	v, ok := n.obj[field]
	if !ok || v == nil {
		return nil
	}
	i, ok := toInt32(v)
	if !ok {
		return nil
	}
	return &i
}

func (n *node) f64(field string) float64 {
	f, _ := toFloat64(n.obj[field])
	return f
}

func (n *node) f64Ptr(field string) *float64 {
	v, ok := n.obj[field]
	if !ok || v == nil {
		return nil
	}
	f, ok := toFloat64(v)
	if !ok {
		return nil
	}
	return &f
}

func (n *node) dtPtr(field string) *DateTime {
	s := n.strPtr(field)
	if s == nil {
		return nil
	}
	d := DateTime(*s)
	return &d
}

func toInt32(v any) (int32, bool) {
	switch n := v.(type) {
	case int32:
		return n, true
	case int:
		return int32(n), true
	case int64:
		return int32(n), true
	case float64:
		return int32(n), true
	default:
		return 0, false
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
