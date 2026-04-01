// ==========================================
// ALFYCHAT - ROUTES MÉDIA
// Upload d'avatars, bannières, pièces jointes
// ==========================================

import { Router, Request, Response } from 'express';
import multer from 'multer';
import { authMiddleware, AuthRequest } from '../middleware/auth';
import {
  processImage,
  deleteImage,
  isValidImageType,
  isValidDocumentType,
  saveDocument,
  MAX_FILE_SIZE,
  ImageType,
} from '../utils/image-processor';
import { logger } from '../utils/logger';

export const mediaRouter = Router();

// Configuration multer — stockage en mémoire pour traitement par sharp
const upload = multer({
  storage: multer.memoryStorage(),
  limits: {
    fileSize: MAX_FILE_SIZE,
    files: 1,
  },
  fileFilter: (req, file, cb) => {
    if (isValidImageType(file.mimetype)) {
      cb(null, true);
    } else {
      cb(new Error('Type de fichier non supporté. Utilisez JPEG, PNG, GIF ou WebP.'));
    }
  },
});

// ============ UPLOAD D'AVATAR ============
mediaRouter.post(
  '/upload/avatar',
  authMiddleware,
  upload.single('file'),
  async (req: AuthRequest, res: Response): Promise<void> => {
    try {
      if (!req.file) {
        res.status(400).json({ error: 'Aucun fichier fourni' });
        return;
      }

      const result = await processImage(req.file.buffer, 'avatar', req.userId!);

      logger.info(`Avatar uploadé pour ${req.userId}: ${result.url}`);
      res.json({
        success: true,
        url: result.url,
        width: result.width,
        height: result.height,
        size: result.size,
      });
    } catch (error) {
      logger.error('Erreur upload avatar:', error);
      res.status(500).json({ error: 'Erreur lors du traitement de l\'image' });
    }
  }
);

// ============ UPLOAD DE BANNIÈRE ============
mediaRouter.post(
  '/upload/banner',
  authMiddleware,
  upload.single('file'),
  async (req: AuthRequest, res: Response): Promise<void> => {
    try {
      if (!req.file) {
        res.status(400).json({ error: 'Aucun fichier fourni' });
        return;
      }

      const result = await processImage(req.file.buffer, 'banner', req.userId!);

      logger.info(`Bannière uploadée pour ${req.userId}: ${result.url}`);
      res.json({
        success: true,
        url: result.url,
        width: result.width,
        height: result.height,
        size: result.size,
      });
    } catch (error) {
      logger.error('Erreur upload bannière:', error);
      res.status(500).json({ error: 'Erreur lors du traitement de l\'image' });
    }
  }
);

// ============ UPLOAD DE PIÈCE JOINTE (message) ============
mediaRouter.post(
  '/upload/attachment',
  authMiddleware,
  upload.single('file'),
  async (req: AuthRequest, res: Response): Promise<void> => {
    try {
      if (!req.file) {
        res.status(400).json({ error: 'Aucun fichier fourni' });
        return;
      }

      const result = await processImage(req.file.buffer, 'attachment', req.userId!);

      logger.info(`Pièce jointe uploadée pour ${req.userId}: ${result.url}`);
      res.json({
        success: true,
        url: result.url,
        filename: req.file.originalname,
        width: result.width,
        height: result.height,
        size: result.size,
        mimeType: result.mimeType,
      });
    } catch (error) {
      logger.error('Erreur upload pièce jointe:', error);
      res.status(500).json({ error: 'Erreur lors du traitement de l\'image' });
    }
  }
);

// ============ UPLOAD DE DOCUMENT (pdf, docx, xlsx…) ============
const uploadDoc = multer({
  storage: multer.memoryStorage(),
  limits: { fileSize: MAX_FILE_SIZE, files: 1 },
  fileFilter: (req, file, cb) => {
    if (isValidDocumentType(file.mimetype)) {
      cb(null, true);
    } else {
      cb(new Error('Type de fichier non supporté. Utilisez PDF, DOCX, XLSX, PNG, JPG, etc.'));
    }
  },
});

mediaRouter.post(
  '/upload/document',
  authMiddleware,
  uploadDoc.single('file'),
  async (req: AuthRequest, res: Response): Promise<void> => {
    try {
      if (!req.file) {
        res.status(400).json({ error: 'Aucun fichier fourni' });
        return;
      }

      const { mimetype, originalname, buffer } = req.file;

      // Images → traiter avec sharp pour optimisation
      if (isValidImageType(mimetype)) {
        const result = await processImage(buffer, 'attachment', req.userId!);
        res.json({ success: true, url: result.url, filename: originalname, size: result.size, mimeType: result.mimeType, isImage: true });
        return;
      }

      // Documents → sauvegarder brut
      const result = await saveDocument(buffer, originalname, mimetype, req.userId!);
      logger.info(`Document uploadé pour ${req.userId}: ${result.url}`);
      res.json({ success: true, url: result.url, filename: result.originalName, size: result.size, mimeType: result.mimeType, isImage: false });
    } catch (error) {
      logger.error('Erreur upload document:', error);
      res.status(500).json({ error: 'Erreur lors de l\'enregistrement du fichier' });
    }
  }
);

// ============ UPLOAD D'ICÔNE SERVEUR ============
mediaRouter.post(
  '/upload/icon',
  authMiddleware,
  upload.single('file'),
  async (req: AuthRequest, res: Response): Promise<void> => {
    try {
      if (!req.file) {
        res.status(400).json({ error: 'Aucun fichier fourni' });
        return;
      }

      const result = await processImage(req.file.buffer, 'icon', req.userId!);

      logger.info(`Icône uploadée pour ${req.userId}: ${result.url}`);
      res.json({
        success: true,
        url: result.url,
        width: result.width,
        height: result.height,
        size: result.size,
      });
    } catch (error) {
      logger.error('Erreur upload icône:', error);
      res.status(500).json({ error: 'Erreur lors du traitement de l\'image' });
    }
  }
);

// ============ SUPPRESSION D'IMAGE ============
mediaRouter.delete(
  '/delete',
  authMiddleware,
  async (req: AuthRequest, res: Response): Promise<void> => {
    try {
      const { url } = req.body as { url?: string };

      if (!url) {
        res.status(400).json({ error: 'URL de l\'image requise' });
        return;
      }

      // Vérifier que l'URL contient l'userId (sécurité: on ne peut supprimer que ses propres images)
      if (!url.includes(req.userId!)) {
        res.status(403).json({ error: 'Vous ne pouvez supprimer que vos propres images' });
        return;
      }

      deleteImage(url);
      res.json({ success: true });
    } catch (error) {
      logger.error('Erreur suppression image:', error);
      res.status(500).json({ error: 'Erreur lors de la suppression' });
    }
  }
);

// ============ GESTION DES ERREURS MULTER ============
mediaRouter.use((err: any, req: Request, res: Response, next: any) => {
  if (err instanceof multer.MulterError) {
    if (err.code === 'LIMIT_FILE_SIZE') {
      return res.status(413).json({ error: 'Fichier trop volumineux (max 10 Mo)' });
    }
    return res.status(400).json({ error: err.message });
  }
  if (err) {
    return res.status(400).json({ error: err.message });
  }
  next();
});
