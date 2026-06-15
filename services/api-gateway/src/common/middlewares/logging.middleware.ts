// src/middleware/logger.middleware.ts
import { LoggerService } from '@services/logger.service';
import { Injectable, NestMiddleware } from '@nestjs/common';
import { NextFunction, Request, Response } from 'express';

@Injectable()
export class LoggerMiddleware implements NestMiddleware {
  constructor(private readonly logger: LoggerService) {}
  use(req: Request, res: Response, next: NextFunction) {
    const { method, originalUrl, ip } = req;
    const start = process.hrtime();

    // Add response tracking
    const originalSend = res.send;
    let responseBody: any;

    res.send = function (body) {
      responseBody = body;
      return originalSend.call(this, body);
    };

    res.on('finish', () => {
      const diff = process.hrtime(start);
      const responseTime = (diff[0] * 1e3 + diff[1] * 1e-6).toFixed(3);
      const contentLength = res.get('content-length') || (responseBody ? responseBody.length : 0);

      // Color-coded status codes
      const getStatusColor = (status: number): string => {
        if (status >= 200 && status < 300) return `\x1b[32m${status}\x1b[0m`; // Green
        if (status >= 300 && status < 400) return `\x1b[33m${status}\x1b[0m`; // Yellow
        if (status >= 400 && status < 500) return `\x1b[31m${status}\x1b[0m`; // Red
        if (status >= 500) return `\x1b[35m${status}\x1b[0m`; // Magenta
        return status.toString();
      };

      const coloredStatus = getStatusColor(res.statusCode);
      const msg = `${method} ${originalUrl} ${coloredStatus} - ${responseTime}ms - ${contentLength} - ${ip}`;

      this.logger.log(msg, 'HTTP');
    });
    next();
  }
}
