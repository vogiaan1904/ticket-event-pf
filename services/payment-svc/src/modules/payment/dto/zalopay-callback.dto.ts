export class ZalopayCallbackDataDto {
  app_id: number;
  app_trans_id: string;
  app_time: number;
  app_user: string;
  amount: number;
  embed_data: string; // JSON string
  item: string; // JSON array string
  zp_trans_id: number;
  server_time: number;
  channel: number;
  merchant_user_id: string;
  user_fee_amount: number;
  discount_amount: number;
}

export class ZalopayCallbackDto {
  data: string;
  mac: string;
  type: number; // 1: Order, 2: Agreement
}

export class ZalopayEmbedDataDto {
  redirecturl?: string;
  merchantinfo?: string;
  promotioninfo?: string;
}

export class ZalopayItemDto {
  itemid: string;
  itemname: string;
  itemprice: number;
  itemquantity: number;
}
