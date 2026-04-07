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
import { mediaRouter } from './routes/media';
import { logger } from './utils/logger';

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
  res.json({ status: 'ok', service: 'media' });
});

// Démarrage
const PORT = parseInt(process.env.PORT || '3007');

app.listen(PORT, () => {
  logger.info(`🖼️  Service Média démarré sur le port ${PORT}`);
  logger.info(`   Dossier uploads: ${UPLOAD_DIR}`);
});

export default app;
