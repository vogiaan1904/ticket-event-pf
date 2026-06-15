# Coding Rules & Style Guide for TicketBottle API Gateway

## Table of Contents
1. [Module Structure](#module-structure)
2. [Controller Patterns](#controller-patterns)
3. [Service Patterns](#service-patterns)
4. [DTO Patterns](#dto-patterns)
5. [Mapper Patterns](#mapper-patterns)
6. [Naming Conventions](#naming-conventions)
7. [Import Organization](#import-organization)
8. [Error Handling](#error-handling)
9. [Authentication & Authorization](#authentication--authorization)
10. [gRPC Communication](#grpc-communication)

---

## Module Structure

### Directory Structure
```
src/modules/{resource}/
├── {resource}.controller.ts
├── {resource}.service.ts
├── {resource}.module.ts
├── dtos/
│   ├── req/
│   │   ├── create-{resource}.dto.ts
│   │   ├── update-{resource}.dto.ts
│   │   ├── filter-{resource}.dto.ts
│   │   └── index.ts
│   └── resp/
│       ├── {resource}.resp.dto.ts
│       ├── {nested}.resp.dto.ts
│       └── index.ts
├── mappers/
│   ├── {resource}.mapper.ts
│   └── index.ts
└── enums/
    ├── {property}.enum.ts
    └── index.ts
```

### Module Configuration Pattern
```typescript
import { Module } from '@nestjs/common';
import { ClientsModule, Transport } from '@nestjs/microservices';
import { AppConfigService } from '@/shared/services/config.service';
import { join } from 'path';

@Module({
  imports: [
    ClientsModule.registerAsync([
      {
        name: SERVICE_NAME,
        useFactory: async (config: AppConfigService) => ({
          transport: Transport.GRPC,
          options: {
            url: config.microservicesConfig.serviceUrl,
            package: PACKAGE_NAME,
            protoPath: join(__dirname, '../../protos/{resource}.proto'),
          },
        }),
        inject: [AppConfigService],
      },
    ]),
  ],
  controllers: [ResourceController],
  providers: [ResourceService],
})
export class ResourceModule {}
```

**Rules:**
- ✅ Always use `ClientsModule.registerAsync()` for gRPC clients
- ✅ Use factory pattern with `AppConfigService` injection
- ✅ Proto path must be relative: `join(__dirname, '../../protos/{resource}.proto')`
- ✅ Controllers and services go in separate arrays

---

## Controller Patterns

### Controller Structure
```typescript
import { AccessGuard } from '@/common/guards/access.guard';
import { ResponseDto } from '@/common/interceptors/transfrom.interceptor';
import { RequestWithUser } from '@/shared/types/request-user.type';
import { Body, Controller, Get, Param, Post, Put, Query, Req, UseGuards } from '@nestjs/common';

@Controller('resources')
export class ResourceController {
  constructor(private readonly resourceService: ResourceService) {}

  @Post()
  @UseGuards(AccessGuard)
  @ResponseDto(ResourceRespDto)
  async create(
    @Req() req: RequestWithUser,
    @Body() dto: CreateResourceDto,
  ): Promise<ResourceRespDto> {
    const protoResource = await this.resourceService.create(req.user, dto);
    return ResourceMapper.toDto(protoResource);
  }
}
```

**Rules:**
1. **Decorator Order:**
   - HTTP method decorator (`@Post()`, `@Get()`, etc.) first
   - Guards second (`@UseGuards(AccessGuard)`)
   - Response decorator third (`@ResponseDto()`)
   - Custom decorators last (`@SuccessMessage()`)

2. **Method Signatures:**
   - ✅ All controller methods must be `async`
   - ✅ Return type must be explicitly typed with Promise
   - ✅ Use `@Req() req: RequestWithUser` for authenticated endpoints
   - ✅ Never destructure DTOs in parameters

3. **Controller Logic:**
   - ✅ Controllers should only call service methods and map responses
   - ✅ Always use mappers to convert proto to DTO: `ResourceMapper.toDto(proto)`
   - ✅ Variable naming: use `protoResource` for proto objects, then map to DTO
   - ❌ No business logic in controllers
   - ❌ No direct gRPC calls in controllers

4. **Route Parameters:**
   - ✅ Order: `@Req()`, `@Param()`, `@Query()`, `@Body()`
   - ✅ Use specific parameter names: `@Param('id') id: string`

5. **Section Comments:**
   - ✅ Use comment headers to group related endpoints:
     ```typescript
     // ********************** Resource Config ********************** //
     ```

---

## Service Patterns

### Service Structure
```typescript
import { Inject, Injectable } from '@nestjs/common';
import { ClientGrpc } from '@nestjs/microservices';
import { firstValueFrom } from 'rxjs';
import { SERVICE_NAME, ServiceClient, Resource } from '@/protogen/resource.pb';
import { RequestUser } from '@/shared/types/request-user.type';

@Injectable()
export class ResourceService {
  private resourceService: ServiceClient;
  
  constructor(@Inject(SERVICE_NAME) private resourceServiceClient: ClientGrpc) {}

  public onModuleInit(): void {
    this.resourceService = this.resourceServiceClient.getService<ServiceClient>(SERVICE_NAME);
  }

  async create(user: RequestUser, dto: CreateResourceDto): Promise<Resource> {
    const createResp = await firstValueFrom(
      this.resourceService.create({
        ...dto,
        userId: user.id,
      }),
    );
    
    return createResp.resource;
  }
}
```

**Rules:**
1. **Service Initialization:**
   - ✅ Private property: `private resourceService: ServiceClient`
   - ✅ Constructor injection: `@Inject(SERVICE_NAME)`
   - ✅ Initialize in `onModuleInit()` lifecycle hook
   - ✅ Use generic type: `getService<ServiceClient>(SERVICE_NAME)`

2. **Method Signatures:**
   - ✅ All methods must be `async`
   - ✅ Return proto types (e.g., `Promise<Resource>`), not DTOs
   - ✅ First parameter should be `user: RequestUser` for authenticated operations
   - ✅ Pass resource IDs as separate parameters, not in DTOs

3. **gRPC Calls:**
   - ✅ Always wrap with `firstValueFrom()`
   - ✅ Store response in descriptive variable: `createResp`, `updateResp`, `findOneResp`
   - ✅ Return nested object: `return createResp.resource`
   - ✅ Use spread operator for DTOs: `{ ...dto, userId: user.id }`

4. **Array Handling:**
   - ✅ Provide defaults for optional arrays:
     ```typescript
     categoryIds: dto.categoryIds ? dto.categoryIds : []
     ```

5. **Section Comments:**
   - ✅ Use same comment style as controllers for logical grouping

---

## DTO Patterns

### Request DTOs (Input)

**Create DTOs:**
```typescript
import { IsNotEmpty, IsString, IsOptional, IsUrl, IsArray } from 'class-validator';

export class CreateResourceDto {
  @IsNotEmpty()
  @IsString()
  name: string;

  @IsOptional()
  @IsArray()
  @IsString({ each: true })
  tags?: string[];
}
```

**Update DTOs:**
```typescript
import { IsOptional, IsString, IsUUID } from 'class-validator';
import { Transform, Type } from 'class-transformer';

export class UpdateResourceDto {
  @IsOptional()
  @IsString()
  name?: string;

  @IsOptional()
  @IsArray()
  @IsUUID('4', { each: true })
  @Type(() => String)
  @Transform(({ value }) => value || [])
  categoryIds?: string[];
}
```

**Filter/Query DTOs:**
```typescript
import { IsOptional, IsEnum, IsString } from 'class-validator';

export class FilterResourceDto {
  @IsOptional()
  @IsString()
  searchQuery?: string;

  @IsOptional()
  @IsEnum(ResourceStatus)
  status?: ResourceStatus;
}
```

**Pagination DTO:**
```typescript
import { PaginationQuery } from '@/shared/interfaces/pagination-input.interface';
import { Type } from 'class-transformer';
import { IsNumber, IsOptional, Max, Min } from 'class-validator';

export class PaginationDto implements PaginationQuery {
  @IsNumber()
  @IsOptional()
  @Min(1)
  @Type(() => Number)
  page: number = 1;

  @IsNumber()
  @IsOptional()
  @Min(1)
  @Max(100)
  @Type(() => Number)
  limit: number = 20;
}
```

**Rules:**
1. **Validation Decorators:**
   - ✅ Required fields: `@IsNotEmpty()` first, then type validator
   - ✅ Optional fields: `@IsOptional()` first, then type validator
   - ✅ Use specific validators: `@IsUrl()`, `@IsEmail()`, `@IsUUID()`
   - ✅ Array validation: `@IsArray()` then `@IsString({ each: true })`

2. **Type Transformations:**
   - ✅ Numbers: `@Type(() => Number)`
   - ✅ Booleans: `@Type(() => Boolean)`
   - ✅ Arrays with defaults: `@Transform(({ value }) => value || [])`

3. **Naming:**
   - ✅ Create DTOs: `CreateResourceDto`
   - ✅ Update DTOs: `UpdateResourceDto` (all fields optional)
   - ✅ Filter DTOs: `FilterResourceDto` (all fields optional)
   - ❌ Never use `RequestDto` or `InputDto`

4. **Optional Fields:**
   - ✅ Use `?` for optional properties
   - ✅ Update DTOs: all fields must be optional
   - ✅ Filter DTOs: all fields must be optional

### Response DTOs (Output)

```typescript
export class ResourceRespDto {
  id: string;
  name: string;
  description: string;
  status: ResourceStatus;
  createdAt: Date;
  updatedAt: Date;
  
  // Optional nested objects
  config?: ResourceConfigRespDto;
  owner?: UserRespDto;
}
```

**Rules:**
1. **Naming:**
   - ✅ Must end with `.resp.dto.ts`
   - ✅ Class name: `ResourceRespDto`
   - ✅ Nested objects: `ResourceConfigRespDto`, `ResourceLocationRespDto`

2. **Properties:**
   - ✅ Use plain TypeScript types (no validators)
   - ✅ Date fields should be `Date` type, not string
   - ✅ Use enums from `/enums` directory, not proto enums
   - ✅ Optional nested objects use `?`

3. **Structure:**
   - ✅ Flat for simple responses
   - ✅ Define nested DTOs in same file if small
   - ✅ Separate files for reusable nested DTOs

### Index Files
```typescript
// dtos/req/index.ts
export * from './create-resource.dto';
export * from './update-resource.dto';
export * from './filter-resource.dto';

// dtos/resp/index.ts
export * from './resource.resp.dto';
export * from './config.resp.dto';
```

**Rules:**
- ✅ Always create `index.ts` for barrel exports
- ✅ Export all DTOs from their respective directories
- ✅ Use in controllers: `import { CreateDto, UpdateDto } from './dtos/req'`

---

## Mapper Patterns

### Basic Mapper
```typescript
import { Resource } from '@/protogen/resource.pb';
import { ResourceRespDto } from '../dtos/resp';

export class ResourceMapper {
  static toDto(proto: Resource): ResourceRespDto {
    return {
      id: proto.id,
      name: proto.name,
      status: StatusMapper.toEnum(proto.status),
      createdAt: new Date(proto.createdAt),
      updatedAt: new Date(proto.updatedAt),
      config: proto.config ? ConfigMapper.toDto(proto.config) : undefined,
    };
  }
}
```

### Enum Mapper
```typescript
import { ResourceStatus as ProtoStatus } from '@/protogen/resource.pb';
import { ResourceStatus } from '../enums';

export class StatusMapper {
  private static enumToProtoMap = new Map<ResourceStatus, ProtoStatus>([
    [ResourceStatus.DRAFT, ProtoStatus.RESOURCE_STATUS_DRAFT],
    [ResourceStatus.PUBLISHED, ProtoStatus.RESOURCE_STATUS_PUBLISHED],
  ]);

  private static protoToEnumMap = new Map<ProtoStatus, ResourceStatus>([
    [ProtoStatus.RESOURCE_STATUS_DRAFT, ResourceStatus.DRAFT],
    [ProtoStatus.RESOURCE_STATUS_PUBLISHED, ResourceStatus.PUBLISHED],
  ]);

  static toProto(status: ResourceStatus): ProtoStatus {
    const protoStatus = this.enumToProtoMap.get(status);
    if (!protoStatus) {
      throw new Error(`Unknown ResourceStatus: ${status}`);
    }
    return protoStatus;
  }

  static toEnum(protoStatus: ProtoStatus): ResourceStatus {
    const status = this.protoToEnumMap.get(protoStatus);
    if (!status) {
      throw new Error(`Unknown ProtoStatus: ${protoStatus}`);
    }
    return status;
  }
}
```

**Rules:**
1. **Class Structure:**
   - ✅ Use static classes with static methods only
   - ✅ No constructor
   - ✅ Method name: `toDto()` for proto → DTO conversion
   - ❌ No `fromDto()` or `toProto()` for entities (only for enums)

2. **Conversion Logic:**
   - ✅ Convert dates: `new Date(proto.dateField)`
   - ✅ Convert enums: use enum mappers
   - ✅ Handle optional nested objects with ternary:
     ```typescript
     config: proto.config ? ConfigMapper.toDto(proto.config) : undefined
     ```
   - ✅ Map arrays: `proto.items.map((item) => ItemMapper.toDto(item))`

3. **Enum Mappers:**
   - ✅ Use `Map` for bidirectional conversion
   - ✅ Separate maps: `enumToProtoMap` and `protoToEnumMap`
   - ✅ Throw errors for unknown values
   - ✅ Methods: `toProto()` and `toEnum()`

4. **Index Files:**
   ```typescript
   // mappers/index.ts
   export * from './resource.mapper';
   export * from './config.mapper';
   export * from './status.mapper';
   ```

---

## Naming Conventions

### Files
- ✅ **Controllers:** `{resource}.controller.ts` (singular)
- ✅ **Services:** `{resource}.service.ts` (singular)
- ✅ **Modules:** `{resource}.module.ts` (singular)
- ✅ **Request DTOs:** `{action}-{resource}.dto.ts`
- ✅ **Response DTOs:** `{resource}.resp.dto.ts`
- ✅ **Mappers:** `{resource}.mapper.ts`
- ✅ **Enums:** `{property}.enum.ts`

### Classes
- ✅ **Controllers:** `{Resource}Controller` (PascalCase, singular)
- ✅ **Services:** `{Resource}Service` (PascalCase, singular)
- ✅ **Modules:** `{Resource}Module` (PascalCase, singular)
- ✅ **Request DTOs:** `{Action}{Resource}Dto`
- ✅ **Response DTOs:** `{Resource}RespDto`
- ✅ **Mappers:** `{Resource}Mapper`
- ✅ **Enums:** `{Resource}{Property}` (e.g., `EventStatus`)

### Variables
```typescript
// ✅ Good
const protoEvent = await this.service.create(dto);
const createResp = await firstValueFrom(this.grpcClient.create(req));
const updateResp = await firstValueFrom(this.grpcClient.update(req));
const findOneResp = await firstValueFrom(this.grpcClient.findOne(req));

// ❌ Bad
const event = await this.service.create(dto);  // Ambiguous
const response = await firstValueFrom(...);    // Too generic
const result = await firstValueFrom(...);      // Too generic
```

**Rules:**
- ✅ gRPC responses: `{action}Resp` (e.g., `createResp`, `updateResp`)
- ✅ Proto objects: `proto{Resource}` (e.g., `protoEvent`, `protoConfig`)
- ✅ DTOs: `dto` as parameter name
- ✅ IDs: descriptive names (`userId`, `eventId`, not just `id`)

### Methods
```typescript
// ✅ Controller methods (match HTTP verbs)
async create()
async update()
async findById()
async findMany()
async delete()

// ✅ Service methods (match business operations)
async create()
async update()
async findById()
async findMany()
async delete()

// ❌ Avoid generic names
async get()        // Use findById() or findMany()
async list()       // Use findMany()
async remove()     // Use delete()
```

---

## Import Organization

### Import Order
```typescript
// 1. NestJS core modules
import { Injectable, Inject } from '@nestjs/common';
import { ClientGrpc } from '@nestjs/microservices';

// 2. Third-party libraries
import { firstValueFrom } from 'rxjs';

// 3. Protogen imports (@ alias)
import { EVENT_SERVICE_NAME, EventServiceClient } from '@/protogen/event.pb';

// 4. Shared imports (@ alias)
import { RequestUser } from '@/shared/types/request-user.type';
import { BusinessException } from '@/common/exceptions/business.exception';

// 5. Relative imports (from current module)
import { CreateEventDto, UpdateEventDto } from './dtos/req';
import { EventRespDto } from './dtos/resp';
import { EventMapper } from './mappers';
```

**Rules:**
1. ✅ Group imports by source
2. ✅ Use `@/` alias for absolute imports
3. ✅ Relative imports last
4. ✅ Blank line between groups
5. ✅ Use barrel imports from `index.ts` files
6. ✅ Order: NestJS → Libraries → Protogen → Shared → Relative

---

## Error Handling

### Business Exceptions
```typescript
import { BusinessException } from '@/common/exceptions/business.exception';
import { ErrorCodeEnum } from '@/shared/constants/error-code.constant';

// In service
if (!findOneResp.user) {
  throw new BusinessException(ErrorCodeEnum.UserNotFound);
}

if (user.id !== resourceOwnerId) {
  throw new BusinessException(ErrorCodeEnum.PermissionDenied);
}
```

**Rules:**
1. ✅ Always use `BusinessException` for business logic errors
2. ✅ Use predefined error codes from `ErrorCodeEnum`
3. ✅ Throw exceptions in services, not controllers
4. ✅ Check for null/undefined responses from gRPC
5. ❌ Never use generic `throw new Error()`
6. ❌ Never use HTTP exceptions in services (`BadRequestException`, etc.)

### Null Safety
```typescript
// ✅ Always check gRPC responses
const findOneResp = await firstValueFrom(this.service.findOne({ id }));
if (!findOneResp.user) {
  throw new BusinessException(ErrorCodeEnum.UserNotFound);
}

// ✅ Use optional chaining for arrays
return {
  data: resp.events?.map(EventMapper.toDto) || [],
};

// ✅ Use optional chaining for nested objects
return resp.pagination?.page || 1;
```

---

## Authentication & Authorization

### Protected Routes
```typescript
import { AccessGuard } from '@/common/guards/access.guard';
import { RequestWithUser } from '@/shared/types/request-user.type';

@Controller('resources')
export class ResourceController {
  @Post()
  @UseGuards(AccessGuard)
  @ResponseDto(ResourceRespDto)
  async create(
    @Req() req: RequestWithUser,
    @Body() dto: CreateResourceDto,
  ): Promise<ResourceRespDto> {
    // req.user is available and typed
    const proto = await this.service.create(req.user, dto);
    return ResourceMapper.toDto(proto);
  }
}
```

**Rules:**
1. ✅ Always use `@UseGuards(AccessGuard)` for protected routes
2. ✅ Use `@Req() req: RequestWithUser` type for authenticated requests
3. ✅ Pass `req.user` to service methods as first parameter
4. ✅ Access guard checks JWT and validates user existence
5. ❌ Don't extract `req.user` in controller, pass whole req to service

### Permission Checks
```typescript
// In service
async update(user: RequestUser, id: string, dto: UpdateDto): Promise<Resource> {
  const findOneResp = await firstValueFrom(this.service.findOne({ id }));
  if (!findOneResp.resource) {
    throw new BusinessException(ErrorCodeEnum.ResourceNotFound);
  }

  // Permission check
  if (user.id !== findOneResp.resource.ownerId) {
    throw new BusinessException(ErrorCodeEnum.PermissionDenied);
  }

  // Proceed with update
}
```

---

## gRPC Communication

### Service Client Pattern
```typescript
@Injectable()
export class ResourceService {
  private resourceService: ResourceServiceClient;
  
  constructor(@Inject(RESOURCE_SERVICE_NAME) private client: ClientGrpc) {}

  public onModuleInit(): void {
    this.resourceService = this.client.getService<ResourceServiceClient>(RESOURCE_SERVICE_NAME);
  }
}
```

### Making gRPC Calls
```typescript
// ✅ Good
const createResp = await firstValueFrom(
  this.resourceService.create({
    ...dto,
    userId: user.id,
    categoryIds: dto.categoryIds || [],
  }),
);
return createResp.resource;

// ❌ Bad - missing firstValueFrom
const resp = await this.resourceService.create(dto);

// ❌ Bad - not returning nested object
return createResp;
```

**Rules:**
1. ✅ Always wrap gRPC calls with `firstValueFrom()`
2. ✅ Use spread operator for DTOs
3. ✅ Add user context fields: `userId: user.id`
4. ✅ Provide defaults for arrays: `categoryIds: dto.categoryIds || []`
5. ✅ Return nested proto object: `return resp.resource`
6. ✅ Store response in variable: `const createResp = ...`

---

## Additional Best Practices

### Controller Response Patterns

#### Simple Response
```typescript
@Get(':id')
@UseGuards(AccessGuard)
@ResponseDto(ResourceRespDto)
async findById(@Param('id') id: string): Promise<ResourceRespDto> {
  const proto = await this.service.findById(id);
  return ResourceMapper.toDto(proto);
}
```

#### Paginated Response
```typescript
@Get()
@UseGuards(AccessGuard)
@ResponseDto(FindManyResourceRespDto)
async findMany(
  @Query() pagination: PaginationDto,
  @Query() filter: FilterResourceDto,
): Promise<FindManyResourceRespDto> {
  const resp = await this.service.findMany(filter, pagination);
  return {
    data: resp.resources?.map(ResourceMapper.toDto) || [],
    meta: {
      currentPage: resp.pagination.page,
      perPage: resp.pagination.pageSize,
      total: resp.pagination.count,
      lastPage: resp.pagination.lastPage,
      hasNext: resp.pagination.hasNext,
      hasPrevious: resp.pagination.hasPrevious,
    },
  };
}
```

#### Void Response (with success message)
```typescript
@Delete(':id')
@UseGuards(AccessGuard)
@SuccessMessage('Resource deleted successfully')
async delete(@Req() req: RequestWithUser, @Param('id') id: string): Promise<void> {
  await this.service.delete(req.user, id);
}
```

### Common Patterns

#### Date Handling
```typescript
// ✅ Proto → DTO: Convert to Date
createdAt: new Date(proto.createdAt)

// ✅ DTO → Proto: Send as string (YYYY-MM-DD or ISO)
// Let validation decorators handle format
```

#### Array Handling
```typescript
// ✅ Ensure arrays are never undefined
categoryIds: dto.categoryIds ? dto.categoryIds : []
// or
categoryIds: dto.categoryIds || []
```

#### Nested Objects
```typescript
// ✅ Use ternary for optional nested objects
config: proto.config ? ConfigMapper.toDto(proto.config) : undefined

// ✅ Map arrays of nested objects
categories: proto.categories.map((cat) => ({
  id: cat.id,
  name: cat.name,
}))
```

---

## Code Quality Checklist

Before submitting code, verify:

- [ ] All imports are organized correctly
- [ ] Using `@/` alias for absolute imports
- [ ] Controller methods have proper decorator order
- [ ] All methods are `async` with explicit return types
- [ ] Service methods return proto types, not DTOs
- [ ] Using `firstValueFrom()` for all gRPC calls
- [ ] Mappers are used in controllers, not services
- [ ] Error handling uses `BusinessException`
- [ ] All gRPC responses are null-checked
- [ ] Arrays use `?.map()` with `|| []` fallback
- [ ] DTOs follow naming conventions
- [ ] Request DTOs have validation decorators
- [ ] Response DTOs use clean types (no validators)
- [ ] Index files export all modules
- [ ] Comments separate logical sections
- [ ] Variable names follow conventions (`protoEvent`, `createResp`)
- [ ] Authentication uses `AccessGuard` and `RequestWithUser`

---

## Quick Reference

### Must Use
- ✅ `firstValueFrom()` for gRPC calls
- ✅ `AccessGuard` for protected routes
- ✅ `RequestWithUser` type for authenticated requests
- ✅ `BusinessException` for errors
- ✅ Mappers in controllers for proto → DTO
- ✅ `@/` alias for imports
- ✅ Optional chaining for nullable fields

### Must Not Use
- ❌ Direct gRPC calls without `firstValueFrom()`
- ❌ Generic HTTP exceptions in services
- ❌ DTOs without validation decorators
- ❌ Business logic in controllers
- ❌ Returning proto types from controllers
- ❌ Hardcoded values (use config service)
- ❌ `any` types

---

## Examples Reference

### Complete CRUD Module Example
See: `src/modules/events/` for a complete example following all patterns.

### Minimal Module Example
See: `src/modules/users/` for a minimal implementation.

---

*Last Updated: 2024*
*This guide is based on the existing codebase patterns and should be followed for all new modules.*

