export interface PayOSCreatePaymentLinkRequestBody {
  orderCode: number;
  amount: number;
  description: string;
  cancelUrl: string;
  returnUrl: string;
  signature?: string;
  items?: PayOSItem[];
  buyerName?: string;
  buyerEmail?: string;
  buyerPhone?: string;
  buyerAddress?: string;
  expiredAt?: number;
}

export interface PayOSCreatePaymentLinkResponse {
  bin: string;
  accountNumber: string;
  accountName: string;
  amount: number;
  description: string;
  orderCode: number;
  currency: string;
  paymentLinkId: string;
  status: string;
  checkoutUrl: string;
  qrCode: string;
}

export interface PayOSWebhookData {
  orderCode: number;
  amount: number;
  description: string;
  accountNumber: string;
  reference: string;
  transactionDateTime: string;
  currency: string;
  paymentLinkId: string;
  code: string;
  desc: string;
  counterAccountBankId: string | null;
  counterAccountBankName: string | null;
  counterAccountName: string | null;
  counterAccountNumber: string | null;
  virtualAccountName: string | null;
  virtualAccountNumber: string | null;
}

export interface PayOSWebhookBody {
  code: string;
  desc: string;
  data: PayOSWebhookData;
  signature: string;
}

export interface PayOSWebhookResponse {
  error: number;
  message: string;
  data: any;
}

export interface PayOSItem {
  name: string;
  quantity: number;
  price: number;
}

export interface PayOSReturnData {
  code: string;
  id: string;
  cancel: boolean;
  status: string;
  orderCode: number;
}
