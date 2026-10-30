# AlfyChat — Service Média

Microservice de gestion des fichiers et médias pour AlfyChat.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)
![License](https://img.shields.io/badge/License-Source_Available-blue)

## Rôle

Ce service gère l'upload, le stockage et le traitement des fichiers multimédias : avatars, bannières, icônes de serveur, pièces jointes de messages et documents.

## Stack technique

| Catégorie | Technologies |
|-----------|-------------|
| Langage | Go 1.26 |
| API | net/http (stdlib) |
| Upload | Multer |
| Images | Go (image, libwebp et libavif embarqués via WebAssembly, sans cgo) |
| Auth | JWT |

## Architecture globale

```
Frontend (:4000)  →  Gateway (:3000)  →  Microservices
                                          ├── users    (:3001)
                                          ├── messages  (:3002)
                                          ├── friends   (:3003)
                                          ├── calls     (:3004)
                                          ├── servers   (:3005)
                                          ├── bots      (:3006)
                                          └── media     (:3007)  ← ce service
```

## Démarrage

### Prérequis

- [Go](https://go.dev/) ≥ 1.26

### Variables d'environnement

```env
PORT=3007
JWT_SECRET=
MAX_FILE_SIZE=10485760
UPLOAD_PATH=./uploads
SERVICE_REGISTRY_URL=http://gateway:3000
```

### Développement

```bash
go run .
```

### Build production

```bash
go build -o bin/service .
./bin/service
```

### Tests

```bash
go test ./...
```

### Docker

```bash
docker compose up media
```

## Structure du projet

```
main.go                  # Point d'entrée
internal/
├── media/               # Upload, traitement d'images (WebP), fichiers statiques
└── platform/            # Socle commun aux services Go
```

## Contribution

Voir [CONTRIBUTING.md](./CONTRIBUTING.md).
