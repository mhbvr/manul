package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	listCats   = flag.Bool("list-cats", false, "List all cat IDs")
	listPhotos = flag.Uint64("list-photos", 0, "List photo IDs for cat ID")
	catID      = flag.Uint64("cat-id", 0, "Cat ID for get-photo")
	photoID    = flag.Uint64("photo-id", 0, "Photo ID for get-photo")
	outputFile = flag.String("output", "", "Output file for photo data")
	serverAddr = flag.String("addr", "localhost:8081", "Server address")
)

func main() {
	flag.Parse()

	if *listCats {
		listAllCats()
		return
	}

	if *listPhotos != 0 {
		listPhotosForCat(*listPhotos)
		return
	}

	if *catID != 0 && *photoID != 0 {
		getCatPhoto(*catID, *photoID)
		return
	}

	// Show usage if no flags provided
	flag.Usage()
}

func getClient() pb.CatPhotosServiceClient {
	conn, err := grpc.NewClient(*serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	return pb.NewCatPhotosServiceClient(conn)
}

func listAllCats() {
	client := getClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.ListCats(ctx, &pb.ListCatsRequest{})
	if err != nil {
		log.Fatalf("ListCats failed: %v", err)
	}

	fmt.Println("Cat IDs:")
	for _, catID := range resp.CatIds {
		fmt.Printf("%d\n", catID)
	}
}

func listPhotosForCat(catID uint64) {
	client := getClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
	if err != nil {
		log.Fatalf("ListPhotos failed: %v", err)
	}

	fmt.Printf("Photo IDs for cat %d:\n", catID)
	for _, photoID := range resp.PhotoIds {
		fmt.Printf("%d\n", photoID)
	}
}

func getCatPhoto(catID, photoID uint64) {
	client := getClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.GetPhoto(ctx, &pb.GetPhotoRequest{
		CatId:   catID,
		PhotoId: photoID,
	})
	if err != nil {
		log.Fatalf("GetPhoto failed: %v", err)
	}

	if *outputFile != "" {
		err := ioutil.WriteFile(*outputFile, resp.PhotoData, 0644)
		if err != nil {
			log.Fatalf("Failed to write file: %v", err)
		}
		fmt.Printf("Photo saved to %s (%d bytes)\n", *outputFile, len(resp.PhotoData))
	} else {
		fmt.Printf("Photo data (%d bytes):\n%s\n", len(resp.PhotoData), string(resp.PhotoData))
	}
}