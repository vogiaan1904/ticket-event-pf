import { QueueStatusResponse, SessionStatus } from '@/protogen/waitroom.pb';
import { QueueStatusMapper } from './queue-status.mapper';

const base: QueueStatusResponse = {
  sessionId: 'ss-1',
  status: SessionStatus.SESSION_STATUS_QUEUED,
  position: 3,
  queueLength: 9,
  queuedAt: '',
  expiresAt: '',
  checkoutToken: '',
  checkoutUrl: '',
  checkoutExpiresAt: '',
  admittedAt: '',
  paused: false,
};

describe('QueueStatusMapper.toDto', () => {
  it('tells the waiter the door is paused', () => {
    expect(QueueStatusMapper.toDto({ ...base, paused: true }).paused).toBe(true);
  });

  it('says nothing is paused when the door is open', () => {
    expect(QueueStatusMapper.toDto(base).paused).toBe(false);
  });
});
