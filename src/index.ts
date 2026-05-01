// ==========================================
// ALFYCHAT - SERVICE MÉDIA
// Upload, redimensionnement et stockage d'images
// ==========================================

import dotenv from 'dotenv';
dotenv.config();
import { registerGlobalErrorHandlers } from './utils/error-reporter';
registerGlobalErrorHandlers();
import express from 'express';
import cors from 'cors';
import helmet from 'helmet';
import path from 'path';
import fs from 'fs';
import os from 'os';
import { mediaRouter } from './routes/media';
import { logger } from './utils/logger';
import { metricsMiddleware, collectMetrics } from './middleware/metrics';

dotenv.config();

const app = express();

// Créer les dossiers d'upload
const UPLOAD_DIR = path.join(__dirname, '..', 'uploads');
const UPLOAD_DIRS = ['avatars', 'banners', 'attachments', 'icons', 'wallpapers'];

for (const dir of UPLOAD_DIRS) {
  const dirPath = path.join(UPLOAD_DIR, dir);
  if (!fs.existsSync(dirPath)) {
    fs.mkdirSync(dirPath, { recursive: true });
    logger.info(`Dossier créé: ${dirPath}`);
  }
}

// Middleware de collecte de métriques (avant les routes)
app.use(metricsMiddleware);

// Middleware
app.use(cors({
  origin: process.env.GATEWAY_URL || 'http://localhost:3000',
  credentials: true,
}));
app.use(helmet({
  crossOriginResourcePolicy: { policy: 'cross-origin' },
}));

// Servir les fichiers statiques (images uploadées)
// Hardening : dotfiles refusés, pas de listing de dossier, nosniff, extensions inconnues 404.
app.use('/uploads', express.static(UPLOAD_DIR, {
  maxAge: '7d',
  etag: true,
  lastModified: true,
  dotfiles: 'deny',
  index: false,
  redirect: false,
  setHeaders: (res, filePath) => {
    res.setHeader('Cache-Control', 'public, max-age=604800, immutable');
    res.setHeader('X-Content-Type-Options', 'nosniff');
    res.setHeader('Access-Control-Allow-Origin', process.env.FRONTEND_URL || process.env.GATEWAY_URL || 'http://localhost:4000');
    // Forcer le téléchargement pour TOUS les fichiers (évite XSS inline)
    const safeName = (filePath.split(/[\\/]/).pop() || 'file').replace(/"/g, '');
    res.setHeader('Content-Disposition', `attachment; filename="${safeName}"`);
  },
}));

// Bloquer toute tentative d'accès au dossier racine /uploads (pas de listing).
app.get('/uploads', (_req, res) => res.status(404).end());
app.get('/uploads/', (_req, res) => res.status(404).end());

// Routes API
app.use('/media', mediaRouter);

// Health check
app.get('/health', (req, res) => {
  res.json({ status: 'ok', service: 'media', serviceId: process.env.SERVICE_ID || 'media-default' });
});

// Endpoint métriques (pour le gateway et le monitoring interne)
app.get('/metrics', (req, res) => {
  res.json({
    service: 'media',
    serviceId: process.env.SERVICE_ID || 'media-default',
    location: (process.env.SERVICE_LOCATION || 'EU').toUpperCase(),
    ...collectMetrics(),
    uptime: process.uptime(),
  });
});

const SERVICE_ID = process.env.SERVICE_ID || 'media-default';
const SERVICE_LOCATION = (process.env.SERVICE_LOCATION || 'EU').toUpperCase();
const PORT = parseInt(process.env.PORT || '3007');

// Démarrage
app.listen(PORT, () => {
  logger.info(`🖼️  Service Média démarré sur le port ${PORT}`);
  logger.info(`   ID: ${SERVICE_ID} | Région: ${SERVICE_LOCATION}`);
  logger.info(`   Dossier uploads: ${UPLOAD_DIR}`);
});
