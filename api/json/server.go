// Package jsonapi is the JSON API under /api. openapi.yaml is the contract; api.gen.go is generated from it.
package jsonapi

//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml

import (
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

var log = logging.MustGetLogger("jsonapi")

var (
	//go:embed openapi.yaml
	spec []byte
	//go:embed docs.html
	docs []byte
)

// Server implements ServerInterface. Handlers answer through c.JSON, c.Status or fail.
type Server struct{}

// Register mounts /api, the spec and /api/docs on the engine. Playback runs its torrents on s.
func Register(r *gin.Engine, s *bittorrent.Service) {
	sessions.bts = s
	r.GET("/api/docs", func(c *gin.Context) { c.Data(http.StatusOK, "text/html; charset=utf-8", docs) })
	r.GET("/api/openapi.yaml", func(c *gin.Context) { c.Data(http.StatusOK, "application/yaml", spec) })

	// Auth is a group middleware, not GinServerOptions.Middlewares, so it runs before parameter parsing.
	// Upstream errors can hold URLs with api keys, so a panic is only logged.
	recovery := gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Errorf("panic in %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		abort(c, http.StatusInternalServerError, ErrorCodeInternalError, "internal error")
	})
	group := r.Group("/api", recovery, basicAuth)
	paramError := func(c *gin.Context, err error, status int) {
		if strings.HasPrefix(c.FullPath(), "/api/stream/") {
			fail(c, forbidden) // every stream link rejection looks the same
			return
		}
		abort(c, status, ErrorCodeBadRequest, err.Error())
	}
	RegisterHandlersWithOptions(group, Server{}, GinServerOptions{ErrorHandler: paramError})
	// OpenAPI 3.0 can't declare HEAD; the stream handler serves it like GET (players probe with it).
	group.HEAD("/stream/:id/:file_index", (&ServerInterfaceWrapper{Handler: Server{}, ErrorHandler: paramError}).GetStream)

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
	if (login == "" && password == "") || strings.HasPrefix(c.FullPath(), "/api/stream/") {
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

// failure is an error response.
type failure struct {
	status int
	body   Error
}

func failed(status int, code ErrorCode, message string) *failure {
	return &failure{status, newError(code, message)}
}

func bad(message string) *failure { return failed(http.StatusBadRequest, ErrorCodeBadRequest, message) }

func fail(c *gin.Context, f *failure) { c.AbortWithStatusJSON(f.status, f.body) }

// body decodes the JSON request body; on a malformed one it answers 400 and returns nil.
func body[T any](c *gin.Context) *T {
	var b T
	if err := c.ShouldBindJSON(&b); err != nil {
		fail(c, bad(err.Error()))
		return nil
	}
	return &b
}

// checkParams returns a bad_request failure when page is below 1, limit is outside 1-100, or an enum
// is not a known value, else nil. The generated binding checks none of these. Nil optional enums are skipped.
func checkParams(page, limit *int, enums ...interface{ Valid() bool }) *failure {
	if limit != nil && (*limit < 1 || *limit > 100) {
		return bad("limit must be between 1 and 100")
	}
	if page != nil && *page < 1 {
		return bad("page must be 1 or more")
	}
	for _, v := range enums {
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Pointer && rv.IsNil() {
			continue // unset optional enum
		}
		if !v.Valid() {
			return bad(fmt.Sprintf("invalid value %q", reflect.Indirect(rv).Interface()))
		}
	}
	return nil
}

// GetSystemInfo reports the version, auth and tracker state. It never starts a tracker login.
func (Server) GetSystemInfo(c *gin.Context) {
	info := SystemInfo{
		Version:      ident.GetVersion(),
		AuthRequired: config.Args.LocalLogin != "" || config.Args.LocalPassword != "",
		Language:     config.Get().Language,

		KodiConnected:  config.KodiConnected(),
		SettingsSource: SystemInfoSettingsSource(config.SettingsSource()),
	}
	if p := tracker.Selected(); p != nil {
		info.Tracker = trackerInfo(p)
	}
	c.JSON(http.StatusOK, info)
}

// trackerInfo only checks stored tokens/keys (Authorized); trakt.Authorized() would start a login.
func trackerInfo(p tracker.Provider) *TrackerInfo {
	return &TrackerInfo{Id: p.ID(), Name: p.Name(), Authorized: p.Authorized(), Logins: tracker.Logins(p.ID())}
}
