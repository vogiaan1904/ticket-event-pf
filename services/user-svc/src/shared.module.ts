import { Module, Global } from '@nestjs/common';

import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';

const providers = [AppConfigService, LoggerService];

@Global()
@Module({
  providers,
  exports: [...providers],
})
export class SharedModule {}
