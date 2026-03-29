// ==========================================
// ALFYCHAT - SERVICE DE TRAITEMENT D'IMAGES
// Redimensionnement, conversion WebP, optimisation
// ==========================================

import sharp from 'sharp';
import path from 'path';
import fs from 'fs';
import { v4 as uuidv4 } from 'uuid';
import { logger } from './logger';

const UPLOAD_DIR = path.join(__dirname, '..', '..', 'uploads');

// Identité de cette instance média (injectée via variables d'environnement)
const SERVICE_ID = process.env.SERVICE_ID || 'media-default';
const SERVICE_LOCATION = (process.env.SERVICE_LOCATION || 'EU').toUpperCase();

// Tailles d'images par type
const IMAGE_SIZES = {
  avatar: { width: 256, height: 256, fit: 'cover' as const },
  banner: { width: 960, height: 320, fit: 'cover' as const },
  attachment: { width: 1920, height: 1080, fit: 'inside' as const },
  icon: { width: 128, height: 128, fit: 'cover' as const },
};

// Taille max d'upload (10 Mo)
export const MAX_FILE_SIZE = 10 * 1024 * 1024;

export type ImageType = 'avatar' | 'banner' | 'attachment' | 'icon';

export interface ProcessedImage {
  filename: string;
  url: string;
  width: number;
  height: number;
  size: number;
  mimeType: string;
}

/**
 * Traite et optimise une image uploadée
 * - Redimensionne selon le type
 * - Convertit en WebP pour meilleure compression
 * - Sauvegarde sur disque
 */
export async function processImage(
  buffer: Buffer,
  type: ImageType,
  userId: string,
): Promise<ProcessedImage> {
  const config = IMAGE_SIZES[type];
  const folder = type === 'avatar' ? 'avatars'
    : type === 'banner' ? 'banners'
    : type === 'icon' ? 'icons'
    : 'attachments';

  const filename = `${userId}-${uuidv4().slice(0, 8)}.webp`;
  const outputPath = path.join(UPLOAD_DIR, folder, filename);

  // Traitement avec sharp
  const processed = sharp(buffer)
    .resize(config.width, config.height, {
      fit: config.fit,
      withoutEnlargement: true,
    })
    .webp({ quality: 85 });

  const info = await processed.toFile(outputPath);

  // Nouvelle URL : /api/media/:location/:serviceId/:folder/:filename
  // Le gateway utilise ces segments pour router vers la bonne instance.
  const url = `/api/media/${SERVICE_LOCATION}/${SERVICE_ID}/${folder}/${filename}`;

  logger.info(`Image traitée: ${type} → ${filename} (${info.width}x${info.height}, ${info.size} octets)`);

  return {
    filename,
    url,
    width: info.width,
    height: info.height,
    size: info.size,
    mimeType: 'image/webp',
  };
}

/**
 * Supprime une ancienne image du disque
 */
export function deleteImage(imageUrl: string): void {
  if (!imageUrl || !imageUrl.startsWith('/uploads/')) return;

  // Resolve the absolute path and verify it stays within UPLOAD_DIR (path traversal prevention)
  const resolvedPath = path.resolve(UPLOAD_DIR, imageUrl.replace('/uploads/', ''));
  if (!resolvedPath.startsWith(UPLOAD_DIR + path.sep) && resolvedPath !== UPLOAD_DIR) {
    logger.warn(`Tentative de traversée de répertoire bloquée: ${imageUrl}`);
    return;
  }

  if (fs.existsSync(resolvedPath)) {
    fs.unlinkSync(resolvedPath);
    logger.info(`Image supprimée: ${imageUrl}`);
  }
}

/**
 * Valide le type MIME du fichier
 */
export function isValidImageType(mimetype: string): boolean {
  const allowed = ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/avif'];
  return allowed.includes(mimetype);
}
