# AlfyChat — Service Média

Microservice de gestion des fichiers et médias pour AlfyChat.

![Node.js](https://img.shields.io/badge/Bun-1.2-black?logo=bun)
![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript)
![License](https://img.shields.io/badge/License-Source_Available-blue)

## Rôle

Ce service gère l'upload, le stockage et le traitement des fichiers multimédias : avatars, bannières, icônes de serveur, pièces jointes de messages et documents.

## Stack technique

| Catégorie | Technologies |
|-----------|-------------|
| Runtime | Bun |
| Langage | TypeScript |
| API | Express |
| Upload | Multer |
| Images | Sharp |
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

- [Bun](https://bun.sh/) ≥ 1.2

### Variables d'environnement

```env
PORT=3007
JWT_SECRET=
MAX_FILE_SIZE=10485760
UPLOAD_PATH=./uploads
SERVICE_REGISTRY_URL=http://gateway:3000
```

### Installation

```bash
bun install
```

### Développement

```bash
bun run dev
```

### Build production

```bash
bun run build
bun run start
```

### Docker

```bash
docker compose up media
```

## Structure du projet

```
src/
├── index.ts             # Point d'entrée
├── routes/              # Routes Express (avatars, banners, icons, attachments)
├── middleware/          # Auth JWT, validation MIME
└── utils/               # Traitement d'images, nettoyage
uploads/
├── avatars/             # Avatars utilisateurs
├── banners/             # Bannières profil / serveur
├── icons/               # Icônes de serveur
└── attachments/         # Pièces jointes messages
```

## Contribution

Voir [CONTRIBUTING.md](./CONTRIBUTING.md).
