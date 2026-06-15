export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
}

export interface TokenPayload {
  sub: string;
  email: string;
  iat?: number;
  exp?: number;
}

export interface RefreshTokenData {
  userId: string;
  email: string;
  tokenVersion: number;
  createdAt: number;
}
