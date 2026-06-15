package order

import (
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewOrderClient(addr string) (OrderServiceClient, func(), error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Println("gRpc Order client connection failed.", err)
		return nil, nil, err
	}

	log.Println("gRpc Order client connection established.")
	return NewOrderServiceClient(conn), func() { conn.Close() }, nil
}
