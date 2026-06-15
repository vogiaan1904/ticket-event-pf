import { Controller } from '@nestjs/common';
import { OutboxService } from './outbox.service';

@Controller()
export class OutboxController {
  constructor(private readonly outboxService: OutboxService) {}
}
