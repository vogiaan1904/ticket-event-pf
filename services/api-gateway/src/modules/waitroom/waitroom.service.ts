import {
  JoinQueueResponse,
  LeaveQueueResponse,
  PositionUpdate,
  WAITROOM_SERVICE_NAME,
  WaitroomServiceClient,
} from '@/protogen/waitroom.pb';
import { RequestUser } from '@/shared/types/request-user.type';
import { Inject, Injectable } from '@nestjs/common';
import { ClientGrpc } from '@nestjs/microservices';
import { firstValueFrom, Observable } from 'rxjs';
import { JoinQueueDto, LeaveQueueDto } from './dtos/req';

@Injectable()
export class WaitroomService {
  private waitroomService: WaitroomServiceClient;

  constructor(@Inject(WAITROOM_SERVICE_NAME) private waitroomServiceClient: ClientGrpc) {}

  public onModuleInit(): void {
    this.waitroomService =
      this.waitroomServiceClient.getService<WaitroomServiceClient>(WAITROOM_SERVICE_NAME);
  }

  async joinQueue(
    user: RequestUser,
    dto: JoinQueueDto,
    userAgent: string,
    ipAddress: string,
  ): Promise<JoinQueueResponse> {
    const joinQueueResp = await firstValueFrom(
      this.waitroomService.joinQueue({
        userId: user.id,
        eventId: dto.eventId,
        userAgent,
        ipAddress,
      }),
    );

    return joinQueueResp;
  }

  async leaveQueue(dto: LeaveQueueDto): Promise<LeaveQueueResponse> {
    const leaveQueueResp = await firstValueFrom(
      this.waitroomService.leaveQueue({
        sessionId: dto.sessionId,
      }),
    );

    return leaveQueueResp;
  }

  streamQueuePosition(sessionId: string): Observable<PositionUpdate> {
    return this.waitroomService.streamQueuePosition({
      sessionId,
    });
  }
}
