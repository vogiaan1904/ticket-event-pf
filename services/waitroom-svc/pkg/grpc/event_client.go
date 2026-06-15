package grpc

import (
	"log"

	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewEventClient(addr string) (event.EventServiceClient, cleanupFunc, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Println("gRpc Event client connection failed.", err)
		return nil, nil, err
	}

	log.Println("gRpc Event client connection established.")
	return event.NewEventServiceClient(conn), func() { conn.Close() }, nil
}
