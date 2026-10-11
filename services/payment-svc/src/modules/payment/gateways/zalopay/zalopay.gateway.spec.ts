import { HttpService } from '@nestjs/axios';
import { AppConfigService } from '@/shared/services/config.service';
import { CreatePaymentLinkInput } from '../gateway.interface';
import { ZalopayGateWay } from './zalopay.gateway';

const SANDBOX = 'https://sb-openapi.zalopay.vn/v2/create';
const INPUT: CreatePaymentLinkInput = {
  amount: 100000,
  orderCode: 'ORD-1',
  currency: 'VND',
  idempotencyKey: 'key-1',
  redirectUrl: 'https://example.com/done',
  timeoutSeconds: 300,
};

// The real config service reads the environment, so the default is tested too.
function build() {
  const post = jest.fn().mockResolvedValue({ data: { return_code: 1, order_url: 'https://pay/1' } });
  const http = { axiosRef: { post } } as unknown as HttpService;
  return { gateway: new ZalopayGateWay(http, new AppConfigService()), post };
}

describe('ZalopayGateWay.createPaymentLink', () => {
  const saved = process.env.ZALOPAY_CREATE_URL;
  afterEach(() => {
    if (saved === undefined) delete process.env.ZALOPAY_CREATE_URL;
    else process.env.ZALOPAY_CREATE_URL = saved;
  });

  it('posts to the ZaloPay sandbox when no create URL is configured', async () => {
    delete process.env.ZALOPAY_CREATE_URL;
    const { gateway, post } = build();

    await gateway.createPaymentLink(INPUT);

    expect(post).toHaveBeenCalledWith(SANDBOX, expect.anything(), expect.anything());
  });

  it('posts to ZALOPAY_CREATE_URL when it is set', async () => {
    process.env.ZALOPAY_CREATE_URL = 'http://payment-webhook:8080/v2/create';
    const { gateway, post } = build();

    await gateway.createPaymentLink(INPUT);

    expect(post).toHaveBeenCalledWith(
      'http://payment-webhook:8080/v2/create',
      expect.objectContaining({ app_trans_id: expect.stringMatching(/_ORD-1$/) }),
      expect.anything(),
    );
  });
});
