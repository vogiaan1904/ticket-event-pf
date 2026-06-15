import { Module, Global } from '@nestjs/common';

import { AppConfigService } from './shared/services/config.service';
import { LoggerService } from './shared/services/logger.service';

const providers = [AppConfigService, LoggerService];

@Global()
@Module({
  providers,
  exports: [...providers],
})
export class SharedModule {}
