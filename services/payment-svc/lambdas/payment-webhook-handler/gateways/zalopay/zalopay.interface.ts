/**
 * ZaloPay interfaces based on official documentation
 */

export interface ZaloCreatePaymentUrlRequestBody {
  app_id: number;
  app_user: string;
  app_trans_id: string;
  app_time: number;
  expire_duration_seconds: number;
  amount: number;
  description: string;
  callback_url: string;
  sub_app_id?: string;
  item: string;
  embed_data: string;
  mac: string;
  bank_code: string;
}

export interface ZaloCreatePaymentUrlResponse {
  return_code: number;
  return_message: string;
  sub_return_code: number;
  sub_return_message: string;
  zp_trans_token: string;
  order_token: string;
  order_url: string;
  qr_code: string;
}

export interface ZalopayCallbackData {
  app_id: number;
  app_trans_id: string;
  app_time: number;
  app_user: string;
  amount: number;
  embed_data: string;
  item: string;
  zp_trans_id: number;
  server_time: number;
  channel: number;
  merchant_user_id: string;
  user_fee_amount: number;
  discount_amount: number;
}

export interface ZalopayCallbackBody {
  data: string;
  mac: string;
  type: number; // 1: Order, 2: Agreement
}

export interface ZalopayCallbackResponse {
  return_code: number;
  return_message: string;
}

export interface ZalopayEmbedData {
  redirecturl?: string;
  merchantinfo?: string;
  promotioninfo?: string;
}

export interface ZalopayItem {
  itemid: string;
  itemname: string;
  itemprice: number;
  itemquantity: number;
}
