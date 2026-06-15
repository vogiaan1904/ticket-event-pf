import { INestApplication } from '@nestjs/common';
import { DocumentBuilder, SwaggerModule } from '@nestjs/swagger';
import { SwaggerTheme, SwaggerThemeNameEnum } from 'swagger-themes';
import { ISwaggerConfig } from '@interfaces/swagger-config.interface';
export function setupSwagger(app: INestApplication, config: ISwaggerConfig) {
  const options = new DocumentBuilder()
    .setTitle(config.title)
    .setDescription(config.description || '')
    .setVersion(config.version)
    .addBearerAuth(
      {
        type: 'http',
        scheme: 'bearer',
        bearerFormat: 'JWT',
      },
      'jwt',
    )
    // .addServer(`${config.scheme}://${config.path}`)
    .addSecurityRequirements('jwt')
    .build();
  const document = SwaggerModule.createDocument(app, options);
  const theme = new SwaggerTheme();

  const mountPath = (config.path || 'docs').replace(/^\/+|\/+$|\s+/g, '');
  SwaggerModule.setup(mountPath, app, document, {
    swaggerOptions: { persistAuthorization: true },
    useGlobalPrefix: true,
    customCss: theme.getBuffer(SwaggerThemeNameEnum.ONE_DARK),
  });
}
