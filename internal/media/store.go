// ==========================================
// ALFYCHAT - STOCKAGE DES IMAGES EN MYSQL
// Les images traitées (WebP) vivent dans la table media_images ; les documents
// restent sur disque. Sans base configurée, tout reste sur disque (tests).
// ==========================================

package media

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/alfychat/media/internal/platform"
)

var imageDB *platform.DB

// UseDatabase active le stockage des images en base.
func UseDatabase(db *platform.DB) { imageDB = db }

// Migrate crée la table des images.
func Migrate(ctx context.Context, db *platform.DB) error {
	_, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS media_images (
        folder VARCHAR(32) NOT NULL,
        filename VARCHAR(255) NOT NULL,
        mime_type VARCHAR(100) NOT NULL DEFAULT 'image/webp',
        size INT NOT NULL,
        data LONGBLOB NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (folder, filename)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	return err
}

// saveImage enregistre une image traitée : en base si elle est active, sinon sur disque.
func saveImage(folder, filename string, data []byte) error {
	if imageDB != nil {
		_, err := imageDB.Exec(context.Background(),
			"INSERT INTO media_images (folder, filename, mime_type, size, data) VALUES (?, ?, 'image/webp', ?, ?)",
			folder, filename, len(data), data)
		return err
	}
	return os.WriteFile(filepath.Join(uploadDir(), folder, filename), data, 0o644)
}

// loadImage lit une image en base ; ok=false si elle n'y est pas.
func loadImage(ctx context.Context, folder, filename string) (data []byte, modTime time.Time, ok bool, err error) {
	if imageDB == nil {
		return nil, time.Time{}, false, nil
	}
	err = imageDB.QueryRowContext(ctx, "SELECT data, created_at FROM media_images WHERE folder = ? AND filename = ?", folder, filename).Scan(&data, &modTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, false, nil
	}
	return data, modTime, err == nil, err
}

// deleteStoredImage supprime une image de la base ; renvoie vrai si une ligne a disparu.
func deleteStoredImage(folder, filename string) bool {
	if imageDB == nil {
		return false
	}
	res, err := imageDB.Exec(context.Background(), "DELETE FROM media_images WHERE folder = ? AND filename = ?", folder, filename)
	if err != nil {
		slog.Error("Suppression image en base", "err", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
