// auth/jwt.strategy.ts
import { Injectable } from '@nestjs/common';
import { PassportStrategy } from '@nestjs/passport';
import { ExtractJwt, Strategy } from 'passport-jwt';
import { AuthService } from '../auth.service';
import { AppConfigService } from '@/shared/services/config.service';
import { TokenPayload } from '@/shared/interfaces/token.interface';
import { RequestUser } from '@/shared/types/request-user.type';

@Injectable()
export class JwtStrategy extends PassportStrategy(Strategy) {
  constructor(
    private authService: AuthService,
    private configService: AppConfigService,
  ) {
    super({
      jwtFromRequest: ExtractJwt.fromAuthHeaderAsBearerToken(),
      ignoreExpiration: false,
      secretOrKey: configService.appConfig.jwtSecret,
    });
  }

  async validate(payload: TokenPayload) {
    await this.authService.validate(payload);

    return {
      id: payload.sub,
      email: payload.email,
    } as RequestUser;
  }
}
