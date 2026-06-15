import { Inject, Injectable } from '@nestjs/common';
import { UpdateUserDto } from './dtos/update-user.dto';
import { USER_SERVICE_NAME, UserServiceClient } from '@/protogen/user.pb';
import { ClientGrpc } from '@nestjs/microservices';
import { firstValueFrom } from 'rxjs';
import { BusinessException } from '@/common/exceptions/business.exception';
import { ErrorCodeEnum } from '@/shared/constants/error-code.constant';
import { UserDto } from './dtos/user.dto';
import { RequestUser } from '@/shared/types/request-user.type';

@Injectable()
export class UsersService {
  private userService: UserServiceClient;
  constructor(@Inject(USER_SERVICE_NAME) private userServiceClient: ClientGrpc) {}

  public onModuleInit(): void {
    this.userService = this.userServiceClient.getService<UserServiceClient>(USER_SERVICE_NAME);
  }

  async update(user: RequestUser, id: string, dto: UpdateUserDto): Promise<UserDto> {
    const findOneResp = await firstValueFrom(this.userService.findOne({ id }));
    if (!findOneResp.user) {
      throw new BusinessException(ErrorCodeEnum.UserNotFound);
    }

    if (user.id !== id) {
      throw new BusinessException(ErrorCodeEnum.PermissionDenied);
    }

    const updateResp = await firstValueFrom(
      this.userService.update({
        id,
        ...dto,
      }),
    );

    return updateResp.user;
  }
}
