// ==========================================
// ALFYCHAT - MÉTRIQUES DU SERVICE
// Collecte RAM, CPU, débit et nombre de requêtes
// ==========================================

import { Request, Response, NextFunction } from 'express';
import os from 'os';
import v8 from 'v8';

// Compteur de requêtes avec fenêtre glissante de 20 minutes
const WINDOW_MS = 20 * 60 * 1000; // 20 minutes
const requestTimestamps: number[] = [];

// Mesure du débit sortant (octets envoyés sur la dernière fenêtre)
let bytesOutWindow: { ts: number; bytes: number }[] = [];

/** Middleware Express : compte chaque requête et mesure le débit */
export function metricsMiddleware(req: Request, res: Response, next: NextFunction): void {
  const now = Date.now();
  requestTimestamps.push(now);

  // Capturer la taille de la réponse
  const originalJson = res.json.bind(res);
  const originalSend = res.send.bind(res);

  const trackBytes = (body: any) => {
    try {
      const size = typeof body === 'string' ? Buffer.byteLength(body) :
        Buffer.isBuffer(body) ? body.length :
        Buffer.byteLength(JSON.stringify(body));
      bytesOutWindow.push({ ts: Date.now(), bytes: size });
    } catch { /* ignore */ }
  };

  res.json = (body: any) => { trackBytes(body); return originalJson(body); };
  res.send = (body: any) => { trackBytes(body); return originalSend(body); };

  next();
}

/** Retourne les métriques courantes du process */
export function collectMetrics(): {
  ramUsage: number;
  ramMax: number;
  cpuUsage: number;
  cpuMax: number;
  bandwidthUsage: number;
  requestCount20min: number;
} {
  const now = Date.now();

  // Purge de la fenêtre glissante
  const cutoff = now - WINDOW_MS;
  while (requestTimestamps.length > 0 && requestTimestamps[0] < cutoff) {
    requestTimestamps.shift();
  }
  bytesOutWindow = bytesOutWindow.filter((e) => e.ts >= cutoff);

  // RAM
  const memUsage = process.memoryUsage();
  const ramUsage = memUsage.heapUsed;
  const totalMem = v8.getHeapStatistics().heap_size_limit;

  // CPU — moyenne sur tous les cœurs depuis le démarrage
  const cpus = os.cpus();
  let totalIdle = 0;
  let totalTick = 0;
  for (const cpu of cpus) {
    for (const type of Object.values(cpu.times)) totalTick += type;
    totalIdle += cpu.times.idle;
  }
  const cpuUsage = totalTick > 0 ? Math.round((1 - totalIdle / totalTick) * 100) : 0;

  // Débit sortant (octets/s sur la fenêtre de 20min → ramené à /s)
  const totalBytes = bytesOutWindow.reduce((s, e) => s + e.bytes, 0);
  const windowSec = WINDOW_MS / 1000;
  const bandwidthUsage = Math.round(totalBytes / windowSec);

  return {
    ramUsage,
    ramMax: totalMem,
    cpuUsage,
    cpuMax: 100,
    bandwidthUsage,
    requestCount20min: requestTimestamps.length,
  };
}
