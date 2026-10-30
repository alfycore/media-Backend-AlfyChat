// ==========================================
// ALFYCHAT - SERVICE DE TRAITEMENT D'IMAGES
// Redimensionnement, conversion WebP, documents.
// ==========================================

package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/alfychat/media/internal/platform"
	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
	"github.com/google/uuid"
	xdraw "golang.org/x/image/draw"
)

// Taille max d'upload (10 Mo) et limite de pixels contre les image-bombs.
const (
	MaxFileSize    = 10 * 1024 * 1024
	maxInputPixels = 100_000_000
)

type imageConfig struct {
	width, height int
	cover         bool // cover (recadrage) ou inside (ajusté dans le cadre)
	folder        string
}

var imageConfigs = map[string]imageConfig{
	"avatar":     {256, 256, true, "avatars"},
	"banner":     {960, 320, true, "banners"},
	"attachment": {1920, 1080, false, "attachments"},
	"icon":       {128, 128, true, "icons"},
	"wallpaper":  {1920, 1080, false, "wallpapers"},
}

// ValidateMagicBytes vérifie le vrai type du fichier. SVG, HTML et ICO sont
// refusés (risque XSS) ainsi que tout contenu qui ne correspond à aucun format.
func ValidateMagicBytes(b []byte) string {
	if len(b) < 12 {
		return ""
	}
	switch {
	case b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "image/jpeg"
	case b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47:
		return "image/png"
	case b[0] == 0x47 && b[1] == 0x49 && b[2] == 0x46 && b[3] == 0x38:
		return "image/gif"
	case string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	case string(b[4:8]) == "ftyp":
		if brand := string(b[8:12]); brand == "avif" || brand == "avis" {
			return "image/avif"
		}
	}
	return ""
}

// IsValidImageType : types MIME d'image acceptés.
func IsValidImageType(mimetype string) bool {
	switch mimetype {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/avif":
		return true
	}
	return false
}

var documentMimes = map[string]string{
	"application/pdf":    "pdf",
	"application/msword": "doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.ms-excel": "xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
	"application/vnd.ms-powerpoint":                                             "ppt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
	"text/plain": "txt",
	"text/csv":   "csv",
	// Variantes courantes sur Windows
	"application/zip":              "docx",
	"application/x-zip-compressed": "docx",
	"application/octet-stream":     "bin",
	"application/x-pdf":            "pdf",
}

var documentExts = map[string]bool{"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true, "pptx": true, "txt": true, "csv": true}

// extOf reproduit path.extname(name).replace('.', ”).toLowerCase().
func extOf(name string) string {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	i := strings.LastIndex(base, ".")
	if i <= 0 {
		return ""
	}
	return strings.ToLower(base[i+1:])
}

// IsValidDocumentType : image, type MIME documentaire connu, ou extension autorisée.
func IsValidDocumentType(mimetype, originalName string) bool {
	if IsValidImageType(mimetype) {
		return true
	}
	if _, ok := documentMimes[mimetype]; ok {
		return true
	}
	if originalName != "" {
		ext := originalName
		if i := strings.LastIndex(ext, "."); i >= 0 {
			ext = ext[i+1:]
		}
		return documentExts[strings.ToLower(ext)]
	}
	return false
}

// ProcessedImage décrit le fichier écrit.
type ProcessedImage struct {
	Filename, URL, MimeType string
	Width, Height, Size     int
}

func jsRound(x float64) int { return int(math.Floor(x + 0.5)) }

// targetDims reproduit les calculs de sharp avec { fit, withoutEnlargement: true }.
// Renvoie la taille après redimensionnement et la taille finale après recadrage.
func targetDims(iw, ih int, cfg imageConfig) (rw, rh, cw, ch int) {
	tw, th := cfg.width, cfg.height
	if cfg.cover {
		// Aucun agrandissement : si l'image est plus petite que le cadre dans
		// une dimension, elle n'est pas redimensionnée, seulement recadrée.
		if iw < tw || ih < th {
			return iw, ih, minInt(iw, tw), minInt(ih, th)
		}
		factor := math.Min(float64(iw)/float64(tw), float64(ih)/float64(th))
		rw, rh = jsRound(float64(iw)/factor), jsRound(float64(ih)/factor)
		return rw, rh, minInt(rw, tw), minInt(rh, th)
	}
	scale := math.Min(1, math.Min(float64(tw)/float64(iw), float64(th)/float64(ih)))
	rw, rh = maxInt(1, jsRound(float64(iw)*scale)), maxInt(1, jsRound(float64(ih)*scale))
	return rw, rh, rw, rh
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// decode lit l'image selon son format réel (déjà validé par les magic bytes).
func decode(buf []byte) (image.Image, error) {
	if ValidateMagicBytes(buf) == "image/avif" {
		return avif.Decode(bytes.NewReader(buf))
	}
	img, _, err := image.Decode(bytes.NewReader(buf))
	return img, err
}

// uploadDir est le dossier racine des fichiers (./uploads).
func uploadDir() string {
	dir, err := filepath.Abs("uploads")
	if err != nil {
		return "uploads"
	}
	return dir
}

func serviceIdentity() (id, location string) {
	return platform.Env("SERVICE_ID", "media-default"), strings.ToUpper(platform.Env("SERVICE_LOCATION", "EU"))
}

var errTooManyPixels = errors.New("Input image exceeds pixel limit")

// ProcessImage redimensionne, convertit en WebP (qualité 85) et enregistre.
func ProcessImage(buf []byte, imgType, userID string) (*ProcessedImage, error) {
	cfg := imageConfigs[imgType]

	// Comme limitInputPixels de sharp : on lit l'en-tête avant de décoder.
	var conf image.Config
	var err error
	if ValidateMagicBytes(buf) == "image/avif" {
		conf, err = avif.DecodeConfig(bytes.NewReader(buf))
	} else {
		conf, _, err = image.DecodeConfig(bytes.NewReader(buf))
	}
	if err != nil {
		return nil, err
	}
	if conf.Width <= 0 || conf.Height <= 0 {
		return nil, errors.New("image vide")
	}
	if conf.Width*conf.Height > maxInputPixels {
		return nil, errTooManyPixels
	}

	src, err := decode(buf)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	rw, rh, cw, ch := targetDims(b.Dx(), b.Dy(), cfg)

	var out image.Image
	switch {
	case rw == b.Dx() && rh == b.Dy():
		out = src
	default:
		dst := image.NewNRGBA(image.Rect(0, 0, rw, rh))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
		out = dst
	}
	if cw != rw || ch != rh {
		// Recadrage centré : (écart + 1) / 2 comme libvips.
		left, top := (rw-cw+1)/2, (rh-ch+1)/2
		crop := image.NewNRGBA(image.Rect(0, 0, cw, ch))
		draw.Draw(crop, crop.Bounds(), out, image.Point{X: out.Bounds().Min.X + left, Y: out.Bounds().Min.Y + top}, draw.Src)
		out = crop
	}

	var enc bytes.Buffer
	if err := webp.Encode(&enc, out, webp.Options{Quality: 85}); err != nil {
		return nil, err
	}

	filename := userID + "-" + uuid.NewString() + ".webp"
	if err := saveImage(cfg.folder, filename, enc.Bytes()); err != nil {
		return nil, err
	}
	id, loc := serviceIdentity()
	url := fmt.Sprintf("/api/media/%s/%s/%s/%s", loc, id, cfg.folder, filename)
	slog.Info(fmt.Sprintf("Image traitée: %s → %s (%dx%d, %d octets)", imgType, filename, cw, ch, enc.Len()))
	return &ProcessedImage{Filename: filename, URL: url, MimeType: "image/webp", Width: cw, Height: ch, Size: enc.Len()}, nil
}

// SavedDocument décrit un document enregistré sans traitement.
type SavedDocument struct {
	Filename, URL, MimeType, OriginalName string
	Size                                  int
}

var (
	unsafeNameRe = regexpMust(`[^a-zA-Z0-9_-]`)
	multiUnderRe = regexpMust(`_+`)
)

// SaveDocument enregistre un document. L'extension vient d'une LISTE BLANCHE :
// un « document » .html / .js / .svg envoyé en application/octet-stream était
// sinon re-servi avec un Content-Type exécutable depuis le domaine de l'API.
func SaveDocument(buf []byte, originalName, mimetype, userID string) (*SavedDocument, error) {
	const folder = "attachments"
	declared := extOf(originalName)
	ext := "bin"
	if documentExts[declared] {
		ext = declared
	} else if m, ok := documentMimes[mimetype]; ok && m != "bin" {
		ext = m
	}

	base := path.Base(strings.ReplaceAll(originalName, `\`, "/"))
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	base = unsafeNameRe.ReplaceAllString(base, "_")
	base = multiUnderRe.ReplaceAllString(base, "_")
	if r := []rune(base); len(r) > 60 {
		base = string(r[:60])
	}
	if base == "" {
		base = "fichier"
	}
	safeName := base + "." + ext
	filename := userID + "-" + uuid.NewString()[:8] + "-" + safeName

	dir := uploadDir()
	out := filepath.Join(dir, folder, filename)
	if !strings.HasPrefix(out, filepath.Join(dir)+string(filepath.Separator)) {
		return nil, errors.New("Chemin de fichier invalide")
	}
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		return nil, err
	}
	id, loc := serviceIdentity()
	url := fmt.Sprintf("/api/media/%s/%s/%s/%s", loc, id, folder, encodeURIComponent(filename))
	slog.Info(fmt.Sprintf("Document sauvegardé: %s (%d octets, .%s) pour %s", filename, len(buf), ext, userID))
	return &SavedDocument{Filename: filename, URL: url, Size: len(buf), MimeType: mimetype, OriginalName: safeName}, nil
}

// DeleteImage supprime un ancien fichier. Deux formats d'URL :
// /uploads/:folder/:filename et /api/media/:location/:serviceId/:folder/:filename.
func DeleteImage(imageURL string) {
	if imageURL == "" {
		return
	}
	var rel string
	switch {
	case strings.HasPrefix(imageURL, "/uploads/"):
		rel = strings.Replace(imageURL, "/uploads/", "", 1)
	default:
		if m := apiMediaRe.FindStringSubmatch(imageURL); m != nil {
			rel = m[1]
		}
	}
	if rel == "" {
		return
	}
	if parts := strings.Split(rel, "/"); len(parts) == 2 && deleteStoredImage(parts[0], parts[1]) {
		slog.Info("Image supprimée: " + imageURL)
		return
	}
	dir := uploadDir()
	resolved := filepath.Join(dir, filepath.FromSlash(rel))
	if !strings.HasPrefix(resolved, dir+string(filepath.Separator)) && resolved != dir {
		slog.Warn("Tentative de traversée de répertoire bloquée: " + imageURL)
		return
	}
	if _, err := os.Stat(resolved); err == nil {
		_ = os.Remove(resolved)
		slog.Info("Image supprimée: " + imageURL)
	}
}

// encodeURIComponent reproduit la fonction JavaScript du même nom.
func encodeURIComponent(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var b strings.Builder
	for _, c := range []byte(s) {
		if strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// latin1 reproduit le décodage par défaut de busboy pour les noms de fichier :
// chaque octet devient un caractère (le texte UTF-8 devient du « mojibake »).
func latin1(raw string) string {
	if utf8.ValidString(raw) && strings.IndexFunc(raw, func(r rune) bool { return r > 0x7f }) < 0 {
		return raw
	}
	rs := make([]rune, len(raw))
	for i := 0; i < len(raw); i++ {
		rs[i] = rune(raw[i])
	}
	return string(rs)
}
