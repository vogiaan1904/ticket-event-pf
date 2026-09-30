package grpc

import (
	"log"

	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/inventory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewInventoryClient(addr string) (inventory.InventoryServiceClient, cleanupFunc, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Println("gRpc Inventory client connection failed.", err)
		return nil, nil, err
	}

	log.Println("gRpc Inventory client connection established.")
	return inventory.NewInventoryServiceClient(conn), func() { conn.Close() }, nil
}
