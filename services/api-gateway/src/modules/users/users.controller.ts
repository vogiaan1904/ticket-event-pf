import { Body, Controller, Param, Patch, Req, UseGuards } from '@nestjs/common';
import { UpdateUserDto } from './dtos/update-user.dto';
import { UsersService } from './users.service';
import { AccessGuard } from '@/common/guards/access.guard';
import { UserDto } from './dtos/user.dto';
import { ResponseDto } from '@/common/interceptors/transfrom.interceptor';

@Controller('users')
export class UsersController {
  constructor(private readonly usersService: UsersService) {}

  @Patch(':id')
  @UseGuards(AccessGuard)
  @ResponseDto(UserDto)
  async update(@Req() req, @Param('id') id: string, @Body() dto: UpdateUserDto): Promise<UserDto> {
    const user = await this.usersService.update(req.user, id, dto);
    return user;
  }
}
