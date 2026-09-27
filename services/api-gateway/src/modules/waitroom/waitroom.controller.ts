import { AccessGuard } from '@/common/guards/access.guard';
import { ResponseDto } from '@/common/interceptors/transfrom.interceptor';
import { RequestWithUser } from '@/shared/types/request-user.type';
import { Body, Controller, Get, Param, Post, Req, UseGuards } from '@nestjs/common';
import { JoinQueueDto, LeaveQueueDto } from './dtos/req';
import { JoinQueueRespDto, LeaveQueueRespDto, QueueStatusRespDto } from './dtos/resp';
import { JoinQueueMapper, LeaveQueueMapper, QueueStatusMapper } from './mappers';
import { WaitroomService } from './waitroom.service';

@Controller('waitroom')
export class WaitroomController {
  constructor(private readonly waitroomService: WaitroomService) {}

  @Post('join')
  @UseGuards(AccessGuard)
  @ResponseDto(JoinQueueRespDto)
  async joinQueue(
    @Req() req: RequestWithUser,
    @Body() dto: JoinQueueDto,
  ): Promise<JoinQueueRespDto> {
    const userAgent = req.headers['user-agent'] || '';
    const ipAddress = (req.ip || req.socket.remoteAddress || '').replace('::ffff:', '');

    const protoResponse = await this.waitroomService.joinQueue(req.user, dto, userAgent, ipAddress);
    return JoinQueueMapper.toDto(protoResponse);
  }

  @Post('leave')
  @UseGuards(AccessGuard)
  @ResponseDto(LeaveQueueRespDto)
  async leaveQueue(
    @Req() req: RequestWithUser,
    @Body() dto: LeaveQueueDto,
  ): Promise<LeaveQueueRespDto> {
    const protoResponse = await this.waitroomService.leaveQueue(req.user, dto);
    return LeaveQueueMapper.toDto(protoResponse);
  }

  // How a waiting user learns their position and, once admitted, their checkout
  // token. Polled: a held connection per waiter does not survive an on-sale.
  @Get('status/:sessionId')
  @UseGuards(AccessGuard)
  @ResponseDto(QueueStatusRespDto)
  async getStatus(
    @Req() req: RequestWithUser,
    @Param('sessionId') sessionId: string,
  ): Promise<QueueStatusRespDto> {
    return QueueStatusMapper.toDto(await this.waitroomService.getQueueStatus(req.user, sessionId));
  }
}
