package waitroom

import (
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewWaitroomClient(addr string) (WaitroomServiceClient, func(), error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Println("gRpc Waitroom client connection failed.", err)
		return nil, nil, err
	}

	log.Println("gRpc Waitroom client connection established.")
	return NewWaitroomServiceClient(conn), func() { conn.Close() }, nil
}
