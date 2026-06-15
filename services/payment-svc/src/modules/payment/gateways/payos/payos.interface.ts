// PayOS interfaces based on official SDK documentation
// SDK: @payos/node

export interface PayOSPaymentRequestBody {
  orderCode: number;
  amount: number;
  description: string;
  items: PayOSItem[];
  cancelUrl: string;
  returnUrl: string;
  expiredAt?: number;
  signature?: string;
  buyerName?: string;
  buyerEmail?: string;
  buyerPhone?: string;
  buyerAddress?: string;
}

export interface PayOSItem {
  name: string;
  quantity: number;
  price: number;
}

export interface PayOSCreatePaymentResponse {
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
  counterAccountBankId: string;
  counterAccountBankName: string;
  counterAccountName: string;
  counterAccountNumber: string;
  virtualAccountName: string;
  virtualAccountNumber: string;
}

export interface PayOSWebhookBody {
  data: PayOSWebhookData;
  signature: string;
}

export interface PayOSCallbackResponse {
  success: boolean;
  message?: string;
}
