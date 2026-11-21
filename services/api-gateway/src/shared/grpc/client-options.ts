import { InterceptingCall, Interceptor } from '@grpc/grpc-js';
import { ORDER_PACKAGE_NAME, ORDER_SERVICE_NAME } from '@/protogen/order.pb';
import { boundedChannelFactory } from './bounded-channel';

// How long a gateway call may run when its method has no longer bound.
export const CALL_DEADLINE_MS = 5_000;

// Above order-svc's own 60s createTimeout, so the gateway never cuts a saga short.
const LONGER_BOUNDS: Record<string, number> = {
  [`/${ORDER_PACKAGE_NAME}.${ORDER_SERVICE_NAME}/CreateOrder`]: 65_000,
};

// deadlineInterceptor gives every call that has none a deadline, by method path.
// Why: docs/decisions/0037-the-gateway-bounds-every-call-it-makes.md.
export const deadlineInterceptor =
  (ms = CALL_DEADLINE_MS, longer = LONGER_BOUNDS): Interceptor =>
  (options, nextCall) => {
    const set = options.deadline !== undefined && options.deadline !== Infinity;
    const bound = longer[options.method_definition.path] ?? ms;
    return new InterceptingCall(
      nextCall(set ? options : { ...options, deadline: Date.now() + bound }),
    );
  };

// clientChannelOptions is every gateway gRPC client's channelOptions.
export const clientChannelOptions = () => ({
  channelFactoryOverride: boundedChannelFactory(),
  interceptors: [deadlineInterceptor()],
});
