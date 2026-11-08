// Package apiv1 is the JSON API under /api/v1. openapi.yaml is the contract; api.gen.go is generated from it.
package apiv1

//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/ident"
)

var log = logging.MustGetLogger("apiv1")

var (
	//go:embed openapi.yaml
	spec []byte
	//go:embed docs.html
	docs []byte
)

// Server implements StrictServerInterface.
//
// The ctx handlers get is the *gin.Context. Gin's ContextWithFallback is off (turning it on would change
// the Kodi handlers), so use ctx.(*gin.Context).Request.Context() for cancellation and .Request for
// http.ServeContent.
type Server struct{}

// Register mounts /api/v1, the spec and /api/docs on the engine. Playback runs its torrents on s.
func Register(r *gin.Engine, s *bittorrent.Service) {
	sessions.bts = s
	r.GET("/api/docs", func(c *gin.Context) { c.Data(http.StatusOK, "text/html; charset=utf-8", docs) })
	r.GET("/api/v1/openapi.yaml", func(c *gin.Context) { c.Data(http.StatusOK, "application/yaml", spec) })

	badRequest := func(c *gin.Context, err error) { abort(c, http.StatusBadRequest, ErrorCodeBadRequest, err.Error()) }
	// Upstream errors can hold URLs with api keys, so they are only logged.
	internal := func(c *gin.Context, err error) {
		log.Errorf("%s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		abort(c, http.StatusInternalServerError, ErrorCodeInternalError, "internal error")
	}
	handler := NewStrictHandlerWithOptions(Server{}, nil, StrictGinServerOptions{
		RequestErrorHandlerFunc:  badRequest,
		HandlerErrorFunc:         internal,
		ResponseErrorHandlerFunc: internal,
	})
	// Auth is a group middleware, not GinServerOptions.Middlewares, so it runs before parameter parsing.
	recovery := gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Errorf("panic in %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		abort(c, http.StatusInternalServerError, ErrorCodeInternalError, "internal error")
	})
	group := r.Group("/api/v1", recovery, basicAuth)
	paramError := func(c *gin.Context, err error, status int) {
		if strings.HasPrefix(c.FullPath(), "/api/v1/stream/") {
			c.AbortWithStatusJSON(http.StatusForbidden, forbidden) // every stream link rejection looks the same
			return
		}
		abort(c, status, ErrorCodeBadRequest, err.Error())
	}
	RegisterHandlersWithOptions(group, handler, GinServerOptions{ErrorHandler: paramError})
	// OpenAPI 3.0 can't declare HEAD; the stream handler serves it like GET (players probe with it).
	group.HEAD("/stream/:id/:file_index", (&ServerInterfaceWrapper{Handler: handler, ErrorHandler: paramError}).GetStream)

	// Without handlers gin still writes its plain 404 for other paths, so Kodi routes are unaffected.
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			abort(c, http.StatusNotFound, ErrorCodeNotFound, "not found")
		}
	})
}

// basicAuth checks the same credentials as the Kodi routes, but answers with a JSON 401 and
// no WWW-Authenticate header, so browsers show no login popup. /stream is authorized by its signed link.
func basicAuth(c *gin.Context) {
	login, password := config.Args.LocalLogin, config.Args.LocalPassword
	if (login == "" && password == "") || strings.HasPrefix(c.FullPath(), "/api/v1/stream/") {
		c.Next()
		return
	}
	u, p, ok := c.Request.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(login))&subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
		abort(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "invalid credentials")
		return
	}
	c.Next()
}

func abort(c *gin.Context, status int, code ErrorCode, message string) {
	c.AbortWithStatusJSON(status, newError(code, message))
}

func newError(code ErrorCode, message string) Error {
	return Error{Error: ErrorBody{Code: code, Message: message}}
}

// checkParams returns a bad_request Error when page is below 1, limit is outside 1-100, or an enum
// is not a known value, else nil. The strict server checks none of these. Nil optional enums are skipped.
func checkParams(page, limit *int, enums ...interface{ Valid() bool }) *Error {
	if limit != nil && (*limit < 1 || *limit > 100) {
		e := newError(ErrorCodeBadRequest, "limit must be between 1 and 100")
		return &e
	}
	if page != nil && *page < 1 {
		e := newError(ErrorCodeBadRequest, "page must be 1 or more")
		return &e
	}
	for _, v := range enums {
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Pointer && rv.IsNil() {
			continue // unset optional enum
		}
		if !v.Valid() {
			e := newError(ErrorCodeBadRequest, fmt.Sprintf("invalid value %q", reflect.Indirect(rv).Interface()))
			return &e
		}
	}
	return nil
}

// GetSystemInfo reports the version, auth and tracker state. It never starts a tracker login.
func (Server) GetSystemInfo(context.Context, GetSystemInfoRequestObject) (GetSystemInfoResponseObject, error) {
	info := GetSystemInfo200JSONResponse{
		Version:      ident.GetVersion(),
		AuthRequired: config.Args.LocalLogin != "" || config.Args.LocalPassword != "",
		Language:     config.Get().Language,

		KodiConnected:  config.KodiConnected(),
		SettingsSource: SystemInfoSettingsSource(config.SettingsSource()),
	}
	if p := tracker.Selected(); p != nil {
		// Authorized() only checks stored tokens/keys for every tracker; trakt.Authorized() would start a login.
		info.Tracker = &TrackerInfo{Id: p.ID(), Name: p.Name(), Authorized: p.Authorized()}
	}
	return info, nil
}
