'use strict';

export interface ISwaggerConfig {
  title: string;
  path?: string;
  description?: string;
  version: string;
  scheme: 'http' | 'https';
}
