// ==========================================
// ALFYCHAT - SERVICE MÉDIA
// Upload, redimensionnement et stockage d'images
// ==========================================

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/alfychat/media/internal/media"
	"github.com/alfychat/media/internal/platform"
)

func main() {
	platform.LoadEnv()
	platform.InitLogger()

	// Dossiers d'upload
	root, _ := filepath.Abs("uploads")
	for _, dir := range []string{"avatars", "banners", "attachments", "icons", "wallpapers"} {
		p := filepath.Join(root, dir)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.MkdirAll(p, 0o755); err != nil {
				platform.Fatal("Création du dossier "+p, err)
			}
			slog.Info("Dossier créé: " + p)
		}
	}

	srv := platform.NewServer()
	// cors({ origin: GATEWAY_URL }) : une origine fixe, annoncée à tous.
	srv.CORSOrigin = platform.Env("GATEWAY_URL", "http://localhost:3000")
	srv.HelmetOverride = map[string]string{"Cross-Origin-Resource-Policy": "cross-origin"}
	// Pas d'analyseur de corps : seule la suppression lit du JSON.
	srv.SkipJSON = func(r *http.Request) bool {
		return !strings.EqualFold(strings.TrimRight(r.URL.Path, "/"), "/media/delete")
	}

	auth := platform.NewAuth()
	auth.Loose = true
	auth.MissingMessage = "Token requis"

	// Les images traitées vivent dans MySQL ; les documents restent sur disque.
	db, err := platform.OpenDatabaseSized("alfychat", "alfychat", platform.EnvInt("DB_POOL_SIZE", 5))
	if err != nil {
		platform.Fatal("Erreur au démarrage", err)
	}
	if err := media.Migrate(context.Background(), db); err != nil {
		platform.Fatal("Erreur au démarrage", err)
	}
	media.UseDatabase(db)

	media.Register(srv, auth)

	id := platform.Env("SERVICE_ID", "media-default")
	loc := strings.ToUpper(platform.Env("SERVICE_LOCATION", "EU"))
	port := platform.Env("PORT", "3007")
	err = srv.Listen(port, func() {
		slog.Info("🖼️  Service Média démarré sur le port " + port)
		slog.Info("   ID: " + id + " | Région: " + loc)
		slog.Info("   Dossier uploads: " + root)
	})
	if err != nil {
		platform.Fatal("Erreur de démarrage", err)
	}
}
