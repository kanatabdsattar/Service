package handlers
import (
	"context"
	"expvar"
	"net/http"
	"net/http/pprof"
	"os"


)

// Options represent optional parameters.
type Options struct {
	corsOrigin string
}

// WithCORS provides configuration options for CORS.
func WithCORS(origin string) func(opts *Options) {
	return func(opts *Options) {
		opts.corsOrigin = origin
	}
}

// DebugStandardLibraryMux registers all the debug routes from the standard library
// into a new mux bypassing the use of the DefaultServerMux. Using the
// DefaultServerMux would be a security risk since a dependency could inject a
// handler into our service without us knowing it.
func DebugStandardLibraryMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Register all the standard library debug endpoints.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/vars", expvar.Handler())

	return mux
}

// DebugMux registers all the debug standard library routes and then custom
// debug application routes for the service. This bypassing the use of the
// DefaultServerMux. Using the DefaultServerMux would be a security risk since
// a dependency could inject a handler into our service without us knowing it.
func DebugMux(build string, log *zap.SugaredLogger, db *sqlx.DB) http.Handler {
	mux := DebugStandardLibraryMux()

	// Register debug check endpoints.
	cg := checkGroup{
		build: build,
		log:   log,
		db:    db,
	}
	mux.HandleFunc("/debug/readiness", cg.readiness)
	mux.HandleFunc("/debug/liveness", cg.liveness)

	return mux
}

// APIMux constructs an http.Handler with all application routes defined.
func APIMux(build string, shutdown chan os.Signal, log *zap.SugaredLogger, metrics *metrics.Metrics, a *auth.Auth, db *sqlx.DB, options ...func(opts *Options)) http.Handler {
	var opts Options
	for _, option := range options {
		option(&opts)
	}

	// Construct the web.App which holds all routes as well as common Middleware.
	app := web.NewApp(shutdown, mid.Logger(log), mid.Errors(log), mid.Metrics(metrics), mid.Panics())

	// Register user management and authentication endpoints.
	ug := userGroup{
		store: user.NewStore(log, db),
		auth:  a,
	}
	app.Handle(http.MethodGet, "/v1/users/:page/:rows", ug.query, mid.Authenticate(a), mid.Authorize(auth.RoleAdmin))
	app.Handle(http.MethodGet, "/v1/users/token/:kid", ug.token)
	app.Handle(http.MethodGet, "/v1/users/:id", ug.queryByID, mid.Authenticate(a))
	app.Handle(http.MethodPost, "/v1/users", ug.create, mid.Authenticate(a), mid.Authorize(auth.RoleAdmin))
	app.Handle(http.MethodPut, "/v1/users/:id", ug.update, mid.Authenticate(a), mid.Authorize(auth.RoleAdmin))
	app.Handle(http.MethodDelete, "/v1/users/:id", ug.delete, mid.Authenticate(a), mid.Authorize(auth.RoleAdmin))

	// Register product and sale endpoints.
	pg := productGroup{
		store: product.NewStore(log, db),
	}
	app.Handle(http.MethodGet, "/v1/products/:page/:rows", pg.query, mid.Authenticate(a))
	app.Handle(http.MethodGet, "/v1/products/:id", pg.queryByID, mid.Authenticate(a))
	app.Handle(http.MethodPost, "/v1/products", pg.create, mid.Authenticate(a))
	app.Handle(http.MethodPut, "/v1/products/:id", pg.update, mid.Authenticate(a))
	app.Handle(http.MethodDelete, "/v1/products/:id", pg.delete, mid.Authenticate(a))

	// Accept CORS 'OPTIONS' preflight requests if config has been provided.
	// Don't forget to apply the CORS middleware to the routes that need it.
	// Example Config: `conf:"default:https://MY_DOMAIN.COM"`
	if opts.corsOrigin != "" {
		h := func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
			return nil
		}
		app.Handle(http.MethodOptions, "/*", h)
	}

	return app
}