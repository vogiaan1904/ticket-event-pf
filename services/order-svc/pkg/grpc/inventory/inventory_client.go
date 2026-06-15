package inventory

import (
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewInventoryClient(addr string) (InventoryServiceClient, func(), error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Println("gRpc Inventory client connection failed.", err)
		return nil, nil, err
	}

	log.Println("gRpc Inventory client connection established.")
	return NewInventoryServiceClient(conn), func() { conn.Close() }, nil
}
