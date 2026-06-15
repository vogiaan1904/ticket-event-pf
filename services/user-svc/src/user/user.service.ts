import { RpcBusinessException } from '@/common/exceptions/rpc-business.exception';
import { ErrorCodeEnum } from '@/shared/constants/error-code.constant';
import { PrismaService } from '@/shared/prisma/prisma.service';
import { Injectable } from '@nestjs/common';
import { User } from '@prisma/client';
import { CreateUserDto } from './dto/create-user.dto';
import { QueryUserDto } from './dto/query-user.dto';
import { UpdateUserDto } from './dto/update-user.dto';
import { UserService } from './user.interface';

@Injectable()
export class UserServiceImpl implements UserService {
  constructor(private readonly prisma: PrismaService) {}

  async create(dto: CreateUserDto): Promise<User> {
    const user = await this.prisma.user.findUnique({
      where: { email: dto.email },
    });
    if (user) {
      throw new RpcBusinessException(ErrorCodeEnum.UserAlreadyExists);
    }

    return this.prisma.user.create({
      data: dto,
    });
  }

  async update(dto: UpdateUserDto): Promise<User> {
    const { id, ...data } = dto;

    const user = await this.prisma.user.findUnique({
      where: { id },
    });
    if (!user) {
      throw new RpcBusinessException(ErrorCodeEnum.UserNotFound);
    }

    return this.prisma.user.update({
      where: { id },
      data,
    });
  }

  async findById(id: string): Promise<User> {
    return await this.prisma.user.findUnique({
      where: { id },
    });
  }

  async findByEmail(email: string): Promise<User> {
    return await this.prisma.user.findUnique({
      where: { email },
    });
  }

  async findAll(dto: QueryUserDto): Promise<User[]> {
    try {
      return this.prisma.user.findMany({
        where: {
          OR: [
            { id: { in: dto.ids } },
            { email: { in: dto.emails } },
            { firstName: { contains: dto.name } },
            { lastName: { contains: dto.name } },
          ],
        },
      });
    } catch (error) {
      console.log(error);
    }
  }
}
