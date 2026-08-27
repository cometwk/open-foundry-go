package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/graph-gophers/graphql-go/relay"

	projgql "github.com/openfoundry/runtime/projection/graphql"
	"github.com/openfoundry/runtime/query"
	"github.com/openfoundry/runtime/spi"
)

const tenantHeader = "X-OpenFoundry-Tenant"

// Handler returns the HTTP mux: POST /graphql, GET by id, and GET follow.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	gql := &relay.Handler{Schema: s.schema}
	mux.Handle("POST /graphql", s.withTenant(gql))
	mux.Handle("GET /api/v1/{type}/{id}/follow", s.withTenant(http.HandlerFunc(s.serveRESTFollow)))
	mux.Handle("GET /api/v1/{type}/{id}", s.withTenant(http.HandlerFunc(s.serveRESTGet)))
	return mux
}

func (s *Server) withTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := strings.TrimSpace(r.Header.Get(tenantHeader))
		if tenant == "" {
			writeError(w, http.StatusBadRequest, "MISSING_TENANT", "X-OpenFoundry-Tenant header is required")
			return
		}
		rc := spi.RequestContext{
			TenantID: tenant,
			ActorID:  sentinelActorID,
			TraceID:  sentinelTraceID,
		}
		next.ServeHTTP(w, r.WithContext(withRC(r.Context(), rc)))
	})
}

func (s *Server) serveRESTGet(w http.ResponseWriter, r *http.Request) {
	seg := r.PathValue("type")
	id := r.PathValue("id")
	typ, ok := s.typeFromPath(seg)
	if !ok {
		writeError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "unknown type")
		return
	}
	computed := []string{}
	res, err := query.Execute(s.engine, rcFrom(r.Context()), query.Op{Get: &query.Get{
		Type:     typ,
		ID:       id,
		Computed: &computed,
	}})
	if err != nil {
		if errors.Is(err, spi.ErrObjectNotFound) {
			writeError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "object not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(publicObject(res.Object))
}

func (s *Server) serveRESTFollow(w http.ResponseWriter, r *http.Request) {
	seg := r.PathValue("type")
	id := r.PathValue("id")
	typ, ok := s.typeFromPath(seg)
	if !ok {
		writeError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "unknown type")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	var fields []string
	if raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				writeError(w, http.StatusBadRequest, "INVALID_FOLLOW_PATH", "empty path")
				return
			}
			fields = append(fields, p)
		}
	}
	res, err := query.Execute(s.engine, rcFrom(r.Context()), query.Op{Expand: &query.Expand{
		StartType:  typ,
		StartID:    id,
		Mode:       query.ExpandTraverse,
		Paths:      [][]string{fields},
		CheckStart: true,
	}})
	if err != nil {
		if errors.Is(err, query.ErrInvalidFollowPath) {
			writeError(w, http.StatusBadRequest, "INVALID_FOLLOW_PATH", err.Error())
			return
		}
		if errors.Is(err, spi.ErrObjectNotFound) {
			writeError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", "object not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	nodes := make([]map[string]any, 0, len(res.Expand.Terminals))
	for _, obj := range res.Expand.Terminals {
		nodes = append(nodes, publicObject(obj))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"nodes": nodes})
}

func (s *Server) typeFromPath(seg string) (string, bool) {
	for _, ot := range s.engine.Ontology().Objects {
		if projgql.LowerFirst(ot.Name) == seg {
			return ot.Name, true
		}
	}
	return "", false
}

func publicObject(obj spi.OntologyObject) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		if spi.IsSystemField(k) {
			continue
		}
		out[k] = v
	}
	if id, ok := obj[spi.FieldID].(string); ok {
		out["id"] = id
	}
	return out
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	_ = json.NewEncoder(w).Encode(body)
}
