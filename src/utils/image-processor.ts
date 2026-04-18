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
  wallpaper: { width: 1920, height: 1080, fit: 'inside' as const },
};

// Taille max d'upload (10 Mo)
export const MAX_FILE_SIZE = 10 * 1024 * 1024;

// Limite de pixels pour éviter les image-bombs (décompression mémoire)
const MAX_INPUT_PIXELS = 100_000_000; // 100MP max → ~400MB RAM max
sharp.cache(false); // Disable caching to prevent memory accumulation

/**
 * Valide les magic bytes du fichier pour vérifier le vrai type MIME.
 * Refuse SVG, ICO, et tout fichier dont le contenu ne correspond pas aux magic bytes.
 */
export function validateMagicBytes(buffer: Buffer): string | null {
  if (buffer.length < 12) return null;

  // JPEG: FF D8 FF
  if (buffer[0] === 0xFF && buffer[1] === 0xD8 && buffer[2] === 0xFF) return 'image/jpeg';
  // PNG: 89 50 4E 47
  if (buffer[0] === 0x89 && buffer[1] === 0x50 && buffer[2] === 0x4E && buffer[3] === 0x47) return 'image/png';
  // GIF: 47 49 46 38
  if (buffer[0] === 0x47 && buffer[1] === 0x49 && buffer[2] === 0x46 && buffer[3] === 0x38) return 'image/gif';
  // WebP: RIFF....WEBP
  if (buffer[0] === 0x52 && buffer[1] === 0x49 && buffer[2] === 0x46 && buffer[3] === 0x46 &&
      buffer[8] === 0x57 && buffer[9] === 0x45 && buffer[10] === 0x42 && buffer[11] === 0x50) return 'image/webp';
  // AVIF: ....ftypavif or ....ftypavis
  if (buffer.length >= 12 && buffer[4] === 0x66 && buffer[5] === 0x74 && buffer[6] === 0x79 && buffer[7] === 0x70) {
    const brand = buffer.toString('ascii', 8, 12);
    if (brand === 'avif' || brand === 'avis') return 'image/avif';
  }

  // SVG/HTML/XML — explicitly reject (XSS risk)
  const head = buffer.toString('utf8', 0, Math.min(512, buffer.length)).toLowerCase();
  if (head.includes('<svg') || head.includes('<!doctype') || head.includes('<html')) return null;

  return null;
}

export type ImageType = 'avatar' | 'banner' | 'attachment' | 'icon' | 'wallpaper';

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
    : type === 'wallpaper' ? 'wallpapers'
    : 'attachments';

  const filename = `${userId}-${uuidv4()}.webp`;
  const outputPath = path.join(UPLOAD_DIR, folder, filename);

  // Traitement avec sharp — limiter les pixels en entrée pour prévenir les image-bombs
  const processed = sharp(buffer, { limitInputPixels: MAX_INPUT_PIXELS })
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
 * Supprime une ancienne image du disque.
 * Supporte deux formats d'URL :
 *  - Ancien : /uploads/:folder/:filename
 *  - Nouveau : /api/media/:location/:serviceId/:folder/:filename
 */
export function deleteImage(imageUrl: string): void {
  if (!imageUrl) return;

  let relativePath: string | null = null;

  if (imageUrl.startsWith('/uploads/')) {
    relativePath = imageUrl.replace('/uploads/', '');
  } else {
    // /api/media/:location/:serviceId/:folder/:filename → on garde :folder/:filename
    const match = imageUrl.match(/^\/api\/media\/[^/]+\/[^/]+\/(.+)$/);
    if (match) relativePath = match[1];
  }

  if (!relativePath) return;

  // Sécurité : vérifier que le chemin résolu reste dans UPLOAD_DIR
  const resolvedPath = path.resolve(UPLOAD_DIR, relativePath);
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

/** Types MIME acceptés pour les pièces jointes documentaires */
export const DOCUMENT_MIMES: Record<string, string> = {
  'application/pdf': 'pdf',
  'application/msword': 'doc',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'docx',
  'application/vnd.ms-excel': 'xls',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'xlsx',
  'application/vnd.ms-powerpoint': 'ppt',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'pptx',
  'text/plain': 'txt',
  'text/csv': 'csv',
  // Variantes courantes sur Windows
  'application/zip': 'docx',
  'application/x-zip-compressed': 'docx',
  'application/octet-stream': 'bin',
  'application/x-pdf': 'pdf',
};

/** Extensions de fichiers documentaires autorisées (fallback si MIME générique) */
const DOCUMENT_EXTS = new Set(['pdf','doc','docx','xls','xlsx','ppt','pptx','txt','csv']);

export function isValidDocumentType(mimetype: string, originalName?: string): boolean {
  if (isValidImageType(mimetype)) return true;
  if (mimetype in DOCUMENT_MIMES) return true;
  // Fallback : vérifier par extension si MIME générique
  if (originalName) {
    const ext = originalName.split('.').pop()?.toLowerCase() || '';
    if (DOCUMENT_EXTS.has(ext)) return true;
  }
  return false;
}

export interface SavedDocument {
  filename: string;
  url: string;
  size: number;
  mimeType: string;
  originalName: string;
}

/**
 * Sauvegarde un document (non-image) directement sur disque sans traitement.
 */
export async function saveDocument(
  buffer: Buffer,
  originalName: string,
  mimetype: string,
  userId: string,
): Promise<SavedDocument> {
  const folder = 'attachments';
  const ext = DOCUMENT_MIMES[mimetype] || path.extname(originalName).replace('.', '') || 'bin';
  const safeName = originalName.replace(/[^a-zA-Z0-9._-]/g, '_').slice(0, 80);
  const filename = `${userId}-${uuidv4().slice(0, 8)}-${safeName}`;
  const outputPath = path.join(UPLOAD_DIR, folder, filename);

  // Security: ensure output path stays within UPLOAD_DIR
  const resolved = path.resolve(outputPath);
  if (!resolved.startsWith(path.resolve(UPLOAD_DIR) + path.sep)) {
    throw new Error('Chemin de fichier invalide');
  }

  fs.writeFileSync(outputPath, buffer);

  // Déterminer l'extension réelle depuis le nom de fichier (fallback sur MIME)
  const realExt = path.extname(safeName).replace('.', '').toLowerCase() ||
    DOCUMENT_MIMES[mimetype] ||
    'bin';
  const url = `/api/media/${SERVICE_LOCATION}/${SERVICE_ID}/${folder}/${encodeURIComponent(filename)}`;

  logger.info(`Document sauvegardé: ${filename} (${buffer.length} octets, .${realExt}) pour ${userId}`);

  return { filename, url, size: buffer.length, mimeType: mimetype, originalName: safeName };
}
