import { AccessGuard } from '@/common/guards/access.guard';
import { ResponseDto } from '@/common/interceptors/transfrom.interceptor';
import { RequestWithUser } from '@/shared/types/request-user.type';
import { Body, Controller, Get, Param, Post, Req, Sse, UseGuards } from '@nestjs/common';
import { JoinQueueDto, LeaveQueueDto } from './dtos/req';
import { JoinQueueRespDto, LeaveQueueRespDto } from './dtos/resp';
import { JoinQueueMapper, LeaveQueueMapper, StreamPositionMapper } from './mappers';
import { WaitroomService } from './waitroom.service';
import { from, map, Observable } from 'rxjs';

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
  async leaveQueue(@Body() dto: LeaveQueueDto): Promise<LeaveQueueRespDto> {
    const protoResponse = await this.waitroomService.leaveQueue(dto);
    return LeaveQueueMapper.toDto(protoResponse);
  }

  @Get('position/:sessionId')
  @Sse()
  streamPosition(@Param('sessionId') sessionId: string): Observable<any> {
    return from(this.waitroomService.streamQueuePosition(sessionId)).pipe(
      map((update) => StreamPositionMapper.toDto(update)),
    );
  }
}
