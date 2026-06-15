package payment

import (
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewPaymentClient(addr string) (PaymentServiceClient, func(), error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Println("gRpc Payment client connection failed.", err)
		return nil, nil, err
	}

	log.Println("gRpc Payment client connection established.")
	return NewPaymentServiceClient(conn), func() { conn.Close() }, nil
}
