// ==========================================
// ALFYCHAT - SERVICE MÉDIA
// Upload, redimensionnement et stockage d'images
// ==========================================

import express from 'express';
import cors from 'cors';
import helmet from 'helmet';
import dotenv from 'dotenv';
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
const UPLOAD_DIRS = ['avatars', 'banners', 'attachments', 'icons'];

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
app.use('/uploads', express.static(UPLOAD_DIR, {
  maxAge: '7d',
  etag: true,
  lastModified: true,
  setHeaders: (res) => {
    res.setHeader('Cache-Control', 'public, max-age=604800, immutable');
    res.setHeader('Access-Control-Allow-Origin', '*');
  },
}));

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

// ============ ENREGISTREMENT & HEARTBEAT GATEWAY ============

const GATEWAY_URL = process.env.GATEWAY_URL || 'http://localhost:3000';
const INTERNAL_SECRET = process.env.INTERNAL_SECRET || 'alfychat-internal-secret-dev';
const SERVICE_ID = process.env.SERVICE_ID || 'media-default';
const SERVICE_LOCATION = (process.env.SERVICE_LOCATION || 'EU').toUpperCase();
const PORT = parseInt(process.env.PORT || '3007');

/** Enregistre cette instance auprès du gateway */
async function registerWithGateway(): Promise<void> {
  try {
    const metrics = collectMetrics();
    const endpoint = process.env.SERVICE_ENDPOINT || `http://localhost:${PORT}`;
    const domain = process.env.SERVICE_DOMAIN || `localhost:${PORT}`;

    const res = await fetch(`${GATEWAY_URL}/api/internal/service/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        secret: INTERNAL_SECRET,
        id: SERVICE_ID,
        serviceType: 'media',
        endpoint,
        domain,
        location: SERVICE_LOCATION,
        metrics,
      }),
    });

    if (res.ok) {
      logger.info(`Service média enregistré auprès du gateway (${SERVICE_ID} @ ${endpoint})`);
    } else {
      logger.warn(`Échec de l'enregistrement gateway: ${res.status}`);
    }
  } catch (err) {
    logger.warn('Gateway non disponible pour l\'enregistrement, nouvelle tentative dans 30s…');
  }
}

/** Envoie les métriques au gateway (heartbeat) */
async function sendHeartbeat(): Promise<void> {
  try {
    const metrics = collectMetrics();
    const res = await fetch(`${GATEWAY_URL}/api/internal/service/heartbeat`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ secret: INTERNAL_SECRET, id: SERVICE_ID, metrics }),
    });

    // Si le gateway répond 404, c'est qu'il a redémarré → se ré-enregistrer
    if (res.status === 404) {
      logger.warn('Instance inconnue du gateway, ré-enregistrement…');
      await registerWithGateway();
    }
  } catch {
    // Erreur réseau → pas bloquant
  }
}

// Démarrage
app.listen(PORT, async () => {
  logger.info(`🖼️  Service Média démarré sur le port ${PORT}`);
  logger.info(`   ID: ${SERVICE_ID} | Région: ${SERVICE_LOCATION}`);
  logger.info(`   Dossier uploads: ${UPLOAD_DIR}`);

  // Premier enregistrement avec retry si le gateway n'est pas encore prêt
  let attempts = 0;
  const tryRegister = async () => {
    await registerWithGateway();
    attempts++;
    // Retry jusqu'à 5 fois avec intervalle progressif
    if (attempts < 5) {
      setTimeout(tryRegister, attempts * 10_000);
    }
  };
  // Légère attente pour laisser le gateway démarrer
  setTimeout(tryRegister, 3_000);

  // Heartbeat toutes les 30 secondes
  setInterval(sendHeartbeat, 30_000);
});
