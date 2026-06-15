import {
  CreateUserResponse,
  FindAllUserResponse,
  FindOneUserRequest,
  FindOneUserResponse,
  UpdateUserResponse,
  USER_SERVICE_NAME,
} from '@/protogen/user.pb';
import { LoggerService } from '@/shared/services/logger.service';
import { Controller, Inject } from '@nestjs/common';
import { GrpcMethod } from '@nestjs/microservices';
import { CreateUserDto } from './dto/create-user.dto';
import { QueryUserDto } from './dto/query-user.dto';
import { UpdateUserDto } from './dto/update-user.dto';
import { UserService } from './user.interface';
import { parseUserToPb } from './user.parser';

@Controller()
export class UserController {
  constructor(
    @Inject('UserService') private readonly userService: UserService,
    private readonly logger: LoggerService,
  ) {
    logger.setContext(UserController.name);
  }

  @GrpcMethod(USER_SERVICE_NAME, 'create')
  async create(dto: CreateUserDto): Promise<CreateUserResponse> {
    this.logger.info(`UserController.create called.`);
    const user = await this.userService.create(dto);
    return {
      user: parseUserToPb(user),
    };
  }

  @GrpcMethod(USER_SERVICE_NAME, 'update')
  async update(dto: UpdateUserDto): Promise<UpdateUserResponse> {
    this.logger.info(`UserController.update called.`);
    const user = await this.userService.update(dto);
    return {
      user: parseUserToPb(user),
    };
  }

  @GrpcMethod(USER_SERVICE_NAME, 'findOne')
  async findOne(dto: FindOneUserRequest): Promise<FindOneUserResponse> {
    this.logger.info(`UserController.findOne called.`);
    var user;
    if (dto.email) {
      user = await this.userService.findByEmail(dto.email);
    }
    if (dto.id) {
      user = await this.userService.findById(dto.id);
    }
    return {
      user: parseUserToPb(user),
    };
  }

  @GrpcMethod(USER_SERVICE_NAME, 'findAll')
  async findAll(dto: QueryUserDto): Promise<FindAllUserResponse> {
    this.logger.info(`UserController.findAll called.`);
    const users = await this.userService.findAll(dto);
    return {
      users: users.map((user) => parseUserToPb(user)),
    };
  }
}
