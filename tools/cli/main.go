package main

import (
	"context"
	"flag"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/pavandhadge/vectron/placementdriver/proto/placementdriver"
)

func main() {
	var (
		pdAddr         = flag.String("pd-addr", "localhost:6001", "Placement Driver gRPC address")
		collectionName = flag.String("collection", "my-collection", "Name of the collection to create")
	)
	flag.Parse()

	conn, err := grpc.NewClient(*pdAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewPlacementServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req := &pb.CreateCollectionRequest{
		Name:      *collectionName,
		Dimension: 128, // Example dimension
		Distance:  "euclidean",
	}

	res, err := c.CreateCollection(ctx, req)
	if err != nil {
		log.Fatalf("could not create collection: %v", err)
	}

	if res.Success {
		log.Printf("Successfully created collection '%s'", *collectionName)
	} else {
		log.Printf("Failed to create collection '%s'", *collectionName)
	}
}
