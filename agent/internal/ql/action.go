package ql

// var rc = spi.RequestContext{TenantID: "gold", ActorID: "test"}

// func gqlExec[T any](ctx context.Context, s *api.Server, query string, vars map[string]any) (*T, error) {
// 	res := s.Exec(ctx, rc, query, vars)
// 	if len(res.Errors) > 0 {
// 		return nil, errors.New(res.Errors[0].Message)
// 	}
// 	return res
// }

// func GetChatById(ctx context.Context, s *api.Server, id string) (*Chat, error) {
// 	res := s.Exec(ctx, rc, `
// 		query GetChatById($id: ID!) {
// 			chat(id: $id) {
// 				id
// 				title
// 			}
// 		}
// 	`, nil)

// 	// obj, err := engine.GetObject(spi.RequestContext{TenantID: "gold", ActorID: "test"}, "chat", id)
// 	// if err != nil {
// 	// 	return nil, err
// 	// }
// 	// engine.Traverse(spi.RequestContext{TenantID: "gold", ActorID: "test"}, id, spi.TraversalPath{
// 	// 	Steps: []spi.TraversalStep{
// 	// 		{LinkType: "messages", Direction: "outbound"},
// 	// 	},
// 	// }, nil)
// 	// return parseObjectT[Chat](obj)
// }

// func GetMessagesByChatId(ctx context.Context, engine *engine.Engine, id string) ([]Message, error) {
// 	engine.Traverse(spi.RequestContext{TenantID: "gold", ActorID: "test"}, id, spi.TraversalPath{
// 		Steps: []spi.TraversalStep{
// 			{Type: "chat", ID: id},
// 		},
// 	}, nil)
// 	obj, err := engine.GetObjectOpts()(spi.RequestContext{TenantID: "gold", ActorID: "test"}, "chat", id)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return parseObjectT[[]Message](obj)
// }

// func parseObjectT[T any](obj spi.OntologyObject) (*T, error) {
// 	bytes, err := json.Marshal(obj)
// 	if err != nil {
// 		return nil, err
// 	}
// 	var t T
// 	err = json.Unmarshal(bytes, &t)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &t, nil
// }
