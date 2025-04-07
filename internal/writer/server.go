package writer

import (
	"fmt"
	"import/internal/db"
	"log"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
)

type Writer struct {
	db       *db.DB
	upgrader *websocket.Upgrader
	router   *chi.Mux
}

func NewWriter(db *db.DB) *Writer {
	upgrader := &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins — change for prod!
		},
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(recoveryMiddleware()) // custom recover
	router.Use(corsMiddleware())     // CORS support

	return &Writer{
		db:       db,
		upgrader: upgrader,
		router:   router,
	}
}

func (w *Writer) RegisterHandlers() {
	w.router.Get("/process", w.UploadHandler)
	w.router.Get("/locations", w.GetLocations)
}

func (w *Writer) StartServer(addr string) error {
	w.RegisterHandlers()
	fmt.Println("🚀 Starting server on", addr)
	if err := http.ListenAndServe(addr, w.router); err != nil {
		return fmt.Errorf("error starting server: %v", err)
	}
	return nil
}

// Custom panic recovery middleware (optional, extra logging)
func recoveryMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Printf("🔥 Recovered from panic: %v", err)
					debug.PrintStack()
					http.Error(w, "Internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS middleware using github.com/rs/cors
func corsMiddleware() func(http.Handler) http.Handler {
	return cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}).Handler
}
