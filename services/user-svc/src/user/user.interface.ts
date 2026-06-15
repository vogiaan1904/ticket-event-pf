import { User } from '@prisma/client';
import { QueryUserDto } from './dto/query-user.dto';
import { CreateUserDto } from './dto/create-user.dto';
import { UpdateUserDto } from './dto/update-user.dto';

export interface UserService {
  create(user: CreateUserDto): Promise<User>;
  update(user: UpdateUserDto): Promise<User>;
  findById(id: string): Promise<User>;
  findByEmail(email: string): Promise<User>;
  findAll(query?: QueryUserDto): Promise<Array<User>>;
}
