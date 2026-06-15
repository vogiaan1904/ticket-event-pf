import { SuccessMessage } from '@/common/decorators/success-response.decorator';
import { AccessGuard } from '@/common/guards/access.guard';
import { TokenPair } from '@/shared/interfaces/token.interface';
import { RequestUser, RequestWithUser } from '@/shared/types/request-user.type';
import { Body, Controller, Get, Post, Request, UseGuards } from '@nestjs/common';
import { AuthService } from './auth.service';
import { ChangePasswordDto } from './dtos/change-password.dto';
import { RefreshTokenDto } from './dtos/refresh-token.dto';
import { SigninDto } from './dtos/signin.dto';
import { SignupDto } from './dtos/signup.dto';

@Controller('auth')
export class AuthController {
  constructor(private readonly authService: AuthService) {}

  @Post('signup')
  async signup(@Body() dto: SignupDto): Promise<TokenPair> {
    return this.authService.signup(dto);
  }

  @Post('signin')
  async signin(@Body() dto: SigninDto): Promise<TokenPair> {
    return this.authService.signin(dto);
  }

  @Get('me')
  @UseGuards(AccessGuard)
  async me(@Request() req: RequestWithUser): Promise<RequestUser> {
    return req.user;
  }

  @Post('refresh')
  async refreshToken(@Body() dto: RefreshTokenDto): Promise<TokenPair> {
    return this.authService.refreshAccessToken(dto.refreshToken);
  }

  @Post('logout')
  @UseGuards(AccessGuard)
  @SuccessMessage('Logged out successfully')
  async logout(@Body() dto: RefreshTokenDto): Promise<void> {
    await this.authService.invalidateRefreshToken(dto.refreshToken);
  }

  @Post('logout-all')
  @UseGuards(AccessGuard)
  @SuccessMessage('Logged out from all devices')
  async logoutAll(@Request() req: RequestWithUser): Promise<void> {
    await this.authService.invalidateAllUserTokens(req.user.id);
  }

  @Post('change-password')
  @UseGuards(AccessGuard)
  @SuccessMessage('Password changed successfully')
  async changePassword(
    @Request() req: RequestWithUser,
    @Body() dto: ChangePasswordDto,
  ): Promise<void> {
    await this.authService.changePassword(req.user.id, dto);
  }
}
