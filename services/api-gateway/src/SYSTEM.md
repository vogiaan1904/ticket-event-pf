# TicketBottle API Gateway

## Overview
A NestJS-based API Gateway that serves as the entry point for the TicketBottle event management platform. It handles HTTP requests, authentication, and communicates with backend microservices via gRPC.

## Architecture

### Type
**API Gateway / BFF (Backend for Frontend)**

### Communication Pattern
- **Inbound**: REST API (HTTP/JSON)
- **Outbound**: gRPC (Protocol Buffers)
- **Caching/Session**: Redis

### Core Responsibilities
1. **Request Routing**: Routes REST API calls to appropriate microservices
2. **Authentication & Authorization**: JWT-based auth with refresh tokens stored in Redis
3. **Data Transformation**: Converts between REST DTOs and gRPC protobuf messages
4. **Response Formatting**: Standardized response structure with interceptors
5. **Error Handling**: Global exception filter with business logic exceptions
6. **API Documentation**: Swagger/OpenAPI for development & staging

## Technical Stack

- **Framework**: NestJS 11 (Node.js)
- **Language**: TypeScript
- **Authentication**: JWT (Passport.js) with Argon2 password hashing
- **Validation**: class-validator & class-transformer
- **Session/Cache**: Redis (ioredis)
- **Logging**: Winston with daily log rotation
- **API Docs**: Swagger/OpenAPI
- **Security**: Helmet, CORS, Rate limiting

## Modules

### 1. Auth Module
- User signup/signin with JWT tokens
- Access & refresh token management with sliding window
- Token versioning for security (invalidation on password change)
- Redis-backed refresh token storage

### 2. Users Module
- User profile management
- Communicates with User microservice via gRPC

### 3. Events Module
- Event CRUD operations (create, read, update, delete)
- Event configuration (ticket sales, capacity, visibility)
- Event filtering & pagination
- Event status management (draft, published, cancelled, approved)
- Role-based access control (admin, editor, viewer)
- Communicates with Event microservice via gRPC

## Key Features

### Security
- JWT-based authentication with short-lived access tokens
- Refresh token rotation with versioning
- Password hashing with Argon2
- CORS protection
- Helmet for HTTP headers security

### Request Processing Pipeline
1. **Logging Middleware**: Logs all incoming requests
2. **Validation**: Automatic DTO validation with class-validator
3. **Authentication Guard**: JWT validation for protected routes
4. **Business Logic**: Controller → Service → gRPC client
5. **Response Transformation**: Standardized response format
6. **Exception Handling**: Global error handler with custom business exceptions

### Response Format
```json
{
  "success": true,
  "data": { /* response data */ },
  "message": "Success message"
}
```

### Error Format
```json
{
  "success": false,
  "message": "Error message",
  "code": "ERROR_CODE",
  "details": { /* optional error details */ }
}
```

## Microservices Integration

### Connected Services
1. **User Service** (`user.proto`)
   - User CRUD operations
   - User authentication data

2. **Event Service** (`event.proto`)
   - Event CRUD operations
   - Event configuration
   - Event roles & permissions
   - Event categories & locations

### gRPC Communication
- Protocol Buffer definitions in `/protos`
- Auto-generated TypeScript clients in `/src/protogen`
- Update script: `npm run proto:update`

## Configuration

### Environment Variables
- App settings (port, global prefix, CORS)
- JWT secrets & expiration
- Microservice URLs
- Redis connection
- Database settings
- Swagger configuration

### Key Config Files
- `src/shared/services/config.service.ts`: Centralized configuration
- `.env`: Environment-specific settings

## Development

### Available Scripts
- `npm run start:dev`: Development mode with hot reload
- `npm run proto:update`: Update protobuf definitions from submodule
- `npm run proto:all`: Regenerate TypeScript from proto files

### Logging
- Winston logger with daily rotation
- Separate files for debug and error logs
- Logs stored in `./logs/{environment}/`

### API Documentation
- Available in development & staging
- Default path: `/api/docs`
- Swagger UI with themed interface

## Patterns & Best Practices

### Design Patterns
- **Dependency Injection**: NestJS DI container
- **Decorator Pattern**: Custom decorators for metadata
- **Interceptor Pattern**: Response transformation
- **Guard Pattern**: Route protection
- **Filter Pattern**: Exception handling
- **Mapper Pattern**: DTO ↔ Proto conversions

### Code Organization
```
src/
├── common/           # Shared guards, filters, interceptors, decorators
├── modules/          # Feature modules (auth, users, events)
├── shared/           # Shared services, constants, interfaces, utils
├── protogen/         # Auto-generated gRPC clients
└── main.ts           # Application bootstrap
```

### Custom Decorators
- `@UserContext()`: Extract user from request
- `@ServiceContext()`: Internal service auth
- `@ResponseDto()`: Swagger response documentation
- `@IsYYYYMMDD()`: Date format validation

## Deployment

### Docker Support
- Dockerfile for containerization

### Health & Monitoring
- Winston logging for production monitoring
- Structured JSON logs for parsing
- Request/response logging middleware

## Notes
- API prefix: `/api` (configurable)
- Short-lived JWT tokens for security
- Refresh tokens with sliding window
- Token versioning prevents stale sessions
- All gRPC responses are null-safe (optional chaining)

