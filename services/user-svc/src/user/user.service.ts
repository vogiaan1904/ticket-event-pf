import { RpcBusinessException } from '@/common/exceptions/rpc-business.exception';
import { ErrorCodeEnum } from '@/shared/constants/error-code.constant';
import { PrismaService } from '@/shared/prisma/prisma.service';
import { Injectable } from '@nestjs/common';
import { Prisma, User } from '@prisma/client';
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

    try {
      // `return await`, not `return`: an un-awaited promise settles outside this
      // try block and the catch never runs.
      return await this.prisma.user.create({ data: dto });
    } catch (error) {
      // P2002 = the unique email was taken between the read above and this
      // insert. Only the constraint can see that race.
      if ((error as { code?: string }).code !== 'P2002') throw error;
      throw new RpcBusinessException(ErrorCodeEnum.UserAlreadyExists);
    }
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
    const filters: Prisma.UserWhereInput[] = [];
    if (dto.ids?.length) filters.push({ id: { in: dto.ids } });
    if (dto.emails?.length) filters.push({ email: { in: dto.emails } });
    if (dto.name) {
      filters.push({ firstName: { contains: dto.name } });
      filters.push({ lastName: { contains: dto.name } });
    }

    // Prisma drops an undefined filter, so an unfiltered query would leave an OR
    // of empty conditions -- every user, password hash and all.
    if (filters.length === 0) return [];

    return this.prisma.user.findMany({ where: { OR: filters } });
  }
}
