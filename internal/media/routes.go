// ==========================================
// ALFYCHAT - ROUTES MÉDIA
// Upload d'avatars, bannières, pièces jointes, documents ; fichiers statiques.
// ==========================================

package media

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alfychat/media/internal/platform"
)

func regexpMust(p string) *regexp.Regexp { return regexp.MustCompile(p) }

var (
	apiMediaRe  = regexp.MustCompile(`^/api/media/[^/]+/[^/]+/(.+)$`)
	deleteURLRe = regexp.MustCompile(`^/(?:uploads|api/media)/(?:[a-zA-Z0-9_\-]+/){1,4}([^/]+)$`)
	fileOwnerRe = regexp.MustCompile(`(?i)^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.[a-z0-9]{3,5}$`)
	mimeByExt   = map[string]string{
		"webp": "image/webp", "jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif", "avif": "image/avif",
		"pdf": "application/pdf", "doc": "application/msword",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"xls":  "application/vnd.ms-excel",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"ppt":  "application/vnd.ms-powerpoint",
		"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"txt":  "text/plain; charset=UTF-8", "csv": "text/csv; charset=UTF-8",
	}
)

type handlers struct{ auth *platform.Auth }

// Register déclare les routes ; le serveur de fichiers passe avant le routeur /media.
func Register(srv *platform.Server, auth *platform.Auth) {
	h := &handlers{auth: auth}
	srv.Mount("/media")
	srv.Use(h.serveUploads)

	srv.Handle("GET /uploads", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusNotFound)
	})

	a := auth.Require
	srv.Handle("POST /media/upload/avatar", a(h.uploadImage("avatar", "Avatar uploadé", false)))
	srv.Handle("POST /media/upload/banner", a(h.uploadImage("banner", "Bannière uploadée", false)))
	srv.Handle("POST /media/upload/wallpaper", a(h.uploadImage("wallpaper", "Wallpaper uploadé", false)))
	srv.Handle("POST /media/upload/attachment", a(h.uploadImage("attachment", "Pièce jointe uploadée", true)))
	srv.Handle("POST /media/upload/document", a(h.uploadDocument))
	srv.Handle("POST /media/upload/icon", a(h.uploadImage("icon", "Icône uploadée", false)))
	srv.Handle("DELETE /media/delete", a(h.deleteImage))

	srv.Handle("GET /health", func(w http.ResponseWriter, r *http.Request) {
		id, _ := serviceIdentity()
		platform.JSON(w, 200, platform.O("status", "ok", "service", "media", "serviceId", id))
	})
	srv.Handle("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		id, loc := serviceIdentity()
		body := platform.O("service", "media", "serviceId", id, "location", loc)
		m := platform.CollectServiceMetrics()
		for _, k := range m.Keys() {
			body.Set(k, m.Get(k))
		}
		body.Set("uptime", platform.Uptime())
		platform.JSON(w, 200, body)
	})
}

// ── Fichiers statiques (express.static sur /uploads) ─────────────────────

// keepLastModified rétablit Last-Modified sur les réponses 304 : net/http le
// retire quand un ETag existe, Express garde les deux.
type keepLastModified struct {
	http.ResponseWriter
	modTime time.Time
}

func (k *keepLastModified) WriteHeader(status int) {
	if status == http.StatusNotModified && k.Header().Get("Last-Modified") == "" {
		k.Header().Set("Last-Modified", k.modTime.UTC().Format(http.TimeFormat))
	}
	k.ResponseWriter.WriteHeader(status)
}

type rangeResult int

const (
	rangeServe  rangeResult = iota // une seule plage valide : réponse 206
	rangeIgnore                    // en-tête mal formé ou plages multiples : réponse 200 complète
	rangeUnsatisfiable
)

// rangeVerdict reproduit range-parser + send : plage unique satisfaisable → 206,
// aucune plage satisfaisable → 416, en-tête mal formé ou plusieurs plages → corps complet.
func rangeVerdict(header string, size int64) rangeResult {
	if header == "" {
		return rangeServe
	}
	idx := strings.Index(header, "=")
	if idx == -1 || header[:idx] != "bytes" {
		return rangeIgnore
	}
	n := 0
	for _, spec := range strings.Split(header[idx+1:], ",") {
		parts := strings.SplitN(spec, "-", 2)
		if len(parts) != 2 {
			return rangeIgnore
		}
		start, e1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		end, e2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		switch {
		case e1 != nil && e2 != nil && parts[0] == "" && parts[1] == "":
			continue
		case e1 != nil:
			start, end = size-end, size-1 // « -N » : les N derniers octets
		case e2 != nil:
			end = size - 1
		}
		if end > size-1 {
			end = size - 1
		}
		if (e1 != nil && e2 != nil) || start > end || start < 0 {
			continue
		}
		n++
	}
	switch {
	case n == 0:
		return rangeUnsatisfiable
	case n == 1:
		return rangeServe
	}
	return rangeIgnore
}

// serveUploads sert /uploads : dotfiles refusés, pas de listing, nosniff,
// téléchargement forcé. Les erreurs autres que « introuvable » passent par le
// gestionnaire d'erreurs de l'application, qui répond toujours 500.
func (h *handlers) serveUploads(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.EscapedPath()
	lower := strings.ToLower(p)
	if lower != "/uploads" && !strings.HasPrefix(lower, "/uploads/") {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return true
	}
	sub := p[len("/uploads"):]
	if sub == "" || sub == "/" {
		return true
	}
	// Les erreurs 4xx de send (chemin illisible, octet nul, traversée, dotfile
	// refusé) ne sont pas transmises : serve-static laisse la requête continuer
	// et c'est le 404 standard de l'application qui répond.
	fall := func() bool { return true }
	decoded, err := url.PathUnescape(sub)
	if err != nil || strings.ContainsRune(decoded, 0) {
		return fall()
	}
	segs := strings.FieldsFunc(decoded, func(r rune) bool { return r == '/' || r == '\\' })
	for _, s := range segs {
		if s == ".." || strings.HasPrefix(s, ".") {
			return fall() // traversée ou dotfile refusé (403)
		}
	}
	if strings.HasSuffix(decoded, "/") {
		return true
	}
	var (
		content io.ReadSeeker
		size    int64
		modTime time.Time
		name    string
	)
	// Les images sont d'abord cherchées en base (/uploads/<dossier>/<fichier>), puis sur disque.
	if len(segs) == 2 {
		data, mt, ok, err := loadImage(r.Context(), segs[0], segs[1])
		if err != nil {
			slog.Error("Lecture image en base", "err", err)
			return fall()
		}
		if ok {
			content, size, modTime, name = bytes.NewReader(data), int64(len(data)), mt, segs[1]
		}
	}
	if content == nil {
		full := filepath.Join(uploadDir(), filepath.FromSlash(strings.Trim(decoded, "/")))
		fi, err := os.Stat(full)
		if err != nil || fi.IsDir() {
			return true
		}
		f, err := os.Open(full)
		if err != nil {
			return fall()
		}
		defer f.Close()
		content, name = f, fi.Name()
		size, modTime = fi.Size(), fi.ModTime()
	}

	hd := w.Header()
	etag := fmt.Sprintf(`W/"%x-%x"`, size, modTime.UnixMilli())
	hd.Set("ETag", etag)
	hd.Set("Cache-Control", "public, max-age=604800, immutable")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Access-Control-Allow-Origin", platform.Env("FRONTEND_URL", platform.Env("GATEWAY_URL", "http://localhost:4000")))
	hd.Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	ct := "application/octet-stream"
	if v, ok := mimeByExt[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))]; ok {
		ct = v
	}
	hd.Set("Content-Type", ct)
	switch rangeVerdict(r.Header.Get("Range"), size) {
	case rangeUnsatisfiable:
		// send répond 416 ; l'erreur remonte au gestionnaire d'erreurs, qui répond 500.
		for _, k := range []string{"ETag", "Cache-Control", "X-Content-Type-Options", "Access-Control-Allow-Origin", "Content-Disposition", "Content-Type"} {
			hd.Del(k)
		}
		hd.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		platform.JSON(w, 500, platform.O("error", "Internal server error"))
		return false
	case rangeIgnore:
		r.Header.Del("Range")
	}
	http.ServeContent(&keepLastModified{ResponseWriter: w, modTime: modTime}, r, name, modTime, content)
	return false
}

// ── Réception multipart (multer, stockage en mémoire) ────────────────────

type upload struct {
	originalName string // décodé comme busboy (latin1)
	mimetype     string
	data         []byte
}

type uploadError struct {
	status int
	msg    string
}

// readUpload lit l'unique fichier du champ « file » (multer .single('file')).
// Les erreurs reprennent les statuts et messages du gestionnaire d'erreurs multer.
func readUpload(r *http.Request, filter func(mimetype, name string) bool, filterMsg string) (*upload, *uploadError) {
	ct := r.Header.Get("Content-Type")
	if mt, _, err := mime.ParseMediaType(ct); err != nil || !strings.HasPrefix(mt, "multipart/") {
		return nil, nil // pas du multipart : multer passe la main, req.file reste vide
	}
	mr, err := r.MultipartReader()
	if err != nil {
		if errors.Is(err, http.ErrMissingBoundary) {
			return nil, &uploadError{400, "Multipart: Boundary not found"}
		}
		return nil, &uploadError{400, err.Error()}
	}
	var file *upload
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return file, nil
		}
		if err != nil {
			return nil, &uploadError{400, "Unexpected end of form"}
		}
		_, params, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		rawName, isFile := params["filename"]
		if !isFile {
			_, _ = io.Copy(io.Discard, io.LimitReader(part, 1<<20))
			continue
		}
		if part.FormName() != "file" {
			return nil, &uploadError{400, "Unexpected field"}
		}
		if file != nil {
			return nil, &uploadError{400, "Too many files"}
		}
		// Les octets bruts du nom sont relus comme du latin1 (comportement de busboy).
		name := latin1(rawFilename(part, rawName))
		mimetype := part.Header.Get("Content-Type")
		if mimetype == "" {
			mimetype = "text/plain"
		}
		if !filter(mimetype, name) {
			return nil, &uploadError{400, filterMsg}
		}
		data, err := io.ReadAll(io.LimitReader(part, MaxFileSize+1))
		if err != nil {
			return nil, &uploadError{400, "Unexpected end of form"}
		}
		if len(data) > MaxFileSize {
			return nil, &uploadError{413, "Fichier trop volumineux (max 10 Mo)"}
		}
		file = &upload{originalName: name, mimetype: mimetype, data: data}
	}
}

// rawFilename renvoie les octets du paramètre filename tels qu'envoyés.
func rawFilename(part *multipart.Part, parsed string) string {
	// mime.ParseMediaType conserve déjà les octets UTF-8 d'un nom brut.
	return parsed
}

func fail(w http.ResponseWriter, e *uploadError) { platform.Error(w, e.status, e.msg) }

func userIDOf(r *http.Request) string {
	if id := platform.UserID(r); id != "" {
		return id
	}
	return "undefined" // req.userId! interpolé quand le jeton n'a pas de userId
}

const imageFilterMsg = "Type de fichier non supporté. Utilisez JPEG, PNG, GIF ou WebP."

// uploadImage sert avatar, banner, wallpaper, icon et attachment.
func (h *handlers) uploadImage(imgType, okLog string, withFilename bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, uerr := readUpload(r, func(m, _ string) bool { return IsValidImageType(m) }, imageFilterMsg)
		if uerr != nil {
			fail(w, uerr)
			return
		}
		if file == nil {
			platform.Error(w, 400, "Aucun fichier fourni")
			return
		}
		if ValidateMagicBytes(file.data) == "" {
			platform.Error(w, 400, "Type de fichier invalide — magic bytes non reconnus")
			return
		}
		userID := userIDOf(r)
		res, err := ProcessImage(file.data, imgType, userID)
		if err != nil {
			slog.Error("Erreur upload "+imgType, "err", err)
			platform.Error(w, 500, "Erreur lors du traitement de l'image")
			return
		}
		slog.Info(okLog + " pour " + userID + ": " + res.URL)
		body := platform.O("success", true, "url", res.URL)
		if withFilename {
			body.Set("filename", file.originalName)
		}
		body.Set("width", res.Width).Set("height", res.Height).Set("size", res.Size)
		if withFilename {
			body.Set("mimeType", res.MimeType)
		}
		platform.JSON(w, 200, body)
	}
}

// POST /media/upload/document — pdf, docx, xlsx… ou image.
func (h *handlers) uploadDocument(w http.ResponseWriter, r *http.Request) {
	file, uerr := readUpload(r, IsValidDocumentType, "Type de fichier non supporté. Utilisez PDF, DOCX, XLSX, PNG, JPG, etc.")
	if uerr != nil {
		fail(w, uerr)
		return
	}
	if file == nil {
		platform.Error(w, 400, "Aucun fichier fourni")
		return
	}
	userID := userIDOf(r)
	if IsValidImageType(file.mimetype) {
		if ValidateMagicBytes(file.data) == "" {
			platform.Error(w, 400, "Type de fichier invalide — magic bytes non reconnus")
			return
		}
		res, err := ProcessImage(file.data, "attachment", userID)
		if err != nil {
			slog.Error("Erreur upload document", "err", err)
			platform.Error(w, 500, "Erreur lors de l'enregistrement du fichier")
			return
		}
		platform.JSON(w, 200, platform.O("success", true, "url", res.URL, "filename", file.originalName, "size", res.Size, "mimeType", res.MimeType, "isImage", true))
		return
	}
	res, err := SaveDocument(file.data, file.originalName, file.mimetype, userID)
	if err != nil {
		slog.Error("Erreur upload document", "err", err)
		platform.Error(w, 500, "Erreur lors de l'enregistrement du fichier")
		return
	}
	slog.Info("Document uploadé pour " + userID + ": " + res.URL)
	platform.JSON(w, 200, platform.O("success", true, "url", res.URL, "filename", res.OriginalName, "size", res.Size, "mimeType", res.MimeType, "isImage", false))
}

// DELETE /media/delete — supprime une image dont l'utilisateur est propriétaire.
//
// Le service d'origine lisait req.body sans aucun analyseur de corps : la
// destructuration échouait et la route répondait toujours 500. Le corps JSON
// est ici réellement lu.
func (h *handlers) deleteImage(w http.ResponseWriter, r *http.Request) {
	u, ok := platform.Body(r).Get("url").(string)
	if !ok || u == "" {
		platform.Error(w, 400, "URL de l'image requise")
		return
	}
	userID := platform.UserID(r)
	if userID == "" {
		platform.Error(w, 401, "Non authentifié")
		return
	}
	m := deleteURLRe.FindStringSubmatch(u)
	if m == nil {
		platform.Error(w, 400, "URL invalide")
		return
	}
	owner := fileOwnerRe.FindStringSubmatch(m[1])
	if owner == nil || !strings.EqualFold(owner[1], userID) {
		platform.Error(w, 403, "Vous ne pouvez supprimer que vos propres images")
		return
	}
	DeleteImage(u)
	platform.JSON(w, 200, platform.O("success", true))
}
