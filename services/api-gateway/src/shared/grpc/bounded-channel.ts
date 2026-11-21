import {
  Channel,
  ChannelCredentials,
  ChannelInterface,
  ChannelOptions,
  connectivityState as State,
} from '@grpc/grpc-js';
import { Logger } from '@nestjs/common';

// How long a channel may fail to reach READY before it is replaced.
export const CONNECT_BOUND_MS = 5_000;

// BoundedChannel replaces its grpc-js channel when it stays CONNECTING or
// TRANSIENT_FAILURE past a bound: grpc-js never times out a connect attempt.
// Why: docs/decisions/0035-the-gateway-bounds-a-backend-reconnect.md.
export class BoundedChannel implements ChannelInterface {
  private static readonly logger = new Logger(BoundedChannel.name);
  private inner: Channel;
  private timer?: NodeJS.Timeout;
  private closed = false;

  constructor(
    private readonly target: string,
    private readonly creds: ChannelCredentials,
    private readonly options: ChannelOptions,
    private readonly boundMs: number,
  ) {
    this.inner = this.open();
  }

  private open(): Channel {
    const channel = new Channel(this.target, this.creds, this.options);
    this.watch(channel, channel.getConnectivityState(false));
    return channel;
  }

  private watch(channel: Channel, from: State): void {
    channel.watchConnectivityState(from, Infinity, () => {
      if (this.closed || channel !== this.inner) return;
      const state = channel.getConnectivityState(false);
      this.arm(state === State.CONNECTING || state === State.TRANSIENT_FAILURE);
      if (state !== State.SHUTDOWN) this.watch(channel, state);
    });
  }

  private arm(failing: boolean): void {
    if (!failing) {
      clearTimeout(this.timer);
      this.timer = undefined;
    } else if (!this.timer) {
      this.timer = setTimeout(() => this.replace(), this.boundMs).unref();
    }
  }

  private replace(): void {
    this.timer = undefined;
    const stuck = this.inner;
    BoundedChannel.logger.warn(
      `${this.target}: not READY for ${this.boundMs}ms, replacing the channel`,
    );
    this.inner = this.open();
    stuck.close();
  }

  close(): void {
    this.closed = true;
    clearTimeout(this.timer);
    this.inner.close();
  }

  getTarget() {
    return this.inner.getTarget();
  }

  getConnectivityState(tryToConnect: boolean) {
    return this.inner.getConnectivityState(tryToConnect);
  }

  watchConnectivityState(from: State, deadline: Date | number, cb: (error?: Error) => void) {
    this.inner.watchConnectivityState(from, deadline, cb);
  }

  getChannelzRef() {
    return this.inner.getChannelzRef();
  }

  createCall(...args: Parameters<Channel['createCall']>) {
    return this.inner.createCall(...args);
  }
}

// boundedChannelFactory plugs BoundedChannel into a grpc-js client's options.
export const boundedChannelFactory =
  (boundMs = CONNECT_BOUND_MS) =>
  (target: string, creds: ChannelCredentials, options: ChannelOptions) =>
    new BoundedChannel(target, creds, options, boundMs);
