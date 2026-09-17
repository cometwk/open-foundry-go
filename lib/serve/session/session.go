package session

import "context"

type contextKey string

const sessionKey = contextKey("sessionKey")

func WithSession(ctx context.Context, session any) context.Context {
	// fmt.Println("WithReqID", ctx, reqID)
	return context.WithValue(ctx, sessionKey, session)
}

func GetSession(ctx context.Context) any {
	// fmt.Println("GetReqID", ctx, ctx.Value(reqIDKey))
	if v := ctx.Value(sessionKey); v != nil {
		return v
	}
	return nil
}
