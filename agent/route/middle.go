package route

import (
	"fmt"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v5"
)

const sessionName = "session"

func NewSessionMiddleware(store sessions.Store) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Set("_session_store", store)
			return next(c)
		}
	}
}

func getSession(c *echo.Context) (*sessions.Session, error) {
	store, err := echo.ContextGet[sessions.Store](c, "_session_store")
	if err != nil {
		return nil, fmt.Errorf("failed to get session store: %w", err)
	}
	return store.Get(c.Request(), sessionName)
}

func CreateSession(c *echo.Context, values map[string]interface{}) error {
	sess, err := getSession(c)
	if err != nil {
		return err
	}
	// sess.Options = &sessions.Options{
	// 	Path:     "/",
	// 	MaxAge:   86400 * 7,
	// 	HttpOnly: true,
	// 	SameSite: http.SameSiteNoneMode,
	// }
	for k, v := range values {
		sess.Values[k] = v
	}
	if err := sess.Save(c.Request(), c.Response()); err != nil {
		return err
	}
	return nil
}

func ReadSession(c *echo.Context, key string) (interface{}, error) {
	sess, err := getSession(c)
	if err != nil {
		return nil, err
	}
	return sess.Values[key], nil
}

func HasSession(c *echo.Context, key string) bool {
	sess, err := getSession(c)
	if err != nil {
		return false
	}
	v, ok := sess.Values[key]
	return ok && v != nil
}

func UpdateSession(c *echo.Context, key string, value interface{}) error {
	sess, err := getSession(c)
	if err != nil {
		return err
	}
	sess.Values[key] = value
	return sess.Save(c.Request(), c.Response())
}
