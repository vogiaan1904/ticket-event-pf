import { loadSync, ServiceDefinition } from '@grpc/proto-loader';
import { join } from 'path';
import { GRPC_LOADER_OPTIONS } from './grpc.constant';

describe('GRPC_LOADER_OPTIONS', () => {
  it('decodes a zero value as present, as the generated types promise', () => {
    const def = loadSync(join(__dirname, '../../protos/waitroom.proto'), GRPC_LOADER_OPTIONS);
    const getStatus = (def['waitroom.v1.WaitroomService'] as ServiceDefinition).GetQueueStatus;

    // A Go server leaves every zero value off the wire, a false `paused` included.
    const wire = getStatus.responseSerialize({ sessionId: 'ss-1' });
    const decoded = getStatus.responseDeserialize(wire) as Record<string, unknown>;

    expect(decoded.paused).toBe(false);
    expect(decoded.checkoutToken).toBe('');
  });
});
