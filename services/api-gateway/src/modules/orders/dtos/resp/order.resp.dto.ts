import { OrderStatus } from '../../enums';
import { OrderItemRespDto } from './order-item.resp.dto';

export class OrderRespDto {
  id: string;
  code: string;
  eventId: string;
  userId: string;
  userFullname: string;
  userEmail: string;
  userPhone: string;
  totalAmountCents: number;
  currency: string;
  status: OrderStatus;
  paymentMethod: string;
  items: OrderItemRespDto[];
  createdAt: Date;
  updatedAt: Date;
}
