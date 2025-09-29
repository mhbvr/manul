package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

var (
	listCats    = flag.Bool("list-cats", false, "List all cat IDs")
	listPhotos  = flag.Uint64("list-photos", 0, "List photo IDs for cat ID")
	catID       = flag.Uint64("cat-id", 0, "Cat ID for get-photo")
	photoID     = flag.Uint64("photo-id", 0, "Photo ID for get-photo")
	outputFile  = flag.String("output", "", "Output file for photo data")
	serverAddr  = flag.String("addr", "localhost:8081", "Server address")
	showMetrics = flag.Bool("show-metrics", false, "Show ORCA metrics from trailers")
	width       = flag.Uint("width", 0, "Width for scaling (0 = no scaling)")
	algorithm   = flag.String("algorithm", "BILINEAR", "Scaling algorithm: NEAREST_NEIGHBOR, BILINEAR, CATMULL_ROM, APPROX_BILINEAR")
)

const ORCAMetadataKey = "endpoint-load-metrics-bin"

func getScalingAlgorithm(alg string) pb.ScalingAlgorithm {
	switch alg {
	case "NEAREST_NEIGHBOR":
		return pb.ScalingAlgorithm_NEAREST_NEIGHBOR
	case "BILINEAR":
		return pb.ScalingAlgorithm_BILINEAR
	case "CATMULL_ROM":
		return pb.ScalingAlgorithm_CATMULL_ROM
	case "APPROX_BILINEAR":
		return pb.ScalingAlgorithm_APPROX_BILINEAR
	default:
		return pb.ScalingAlgorithm_BILINEAR
	}
}

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

func printORCAMetrics(trailer metadata.MD) {
	vals := trailer.Get(ORCAMetadataKey)
	if len(vals) == 0 {
		log.Println("No ORCA metrics")
	}

	for _, v := range vals {
		var report v3orcapb.OrcaLoadReport
		if err := proto.Unmarshal([]byte(v), &report); err != nil {
			log.Printf("failed to unmarshal load report found in metadata: %v", err)
		}
		fmt.Printf("ORCA report: %v\n", report.String())
	}
}

func listAllCats() {
	client := getClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var trailer metadata.MD
	resp, err := client.ListCats(ctx, &pb.ListCatsRequest{}, grpc.Trailer(&trailer))
	if err != nil {
		log.Fatalf("ListCats failed: %v", err)
	}

	fmt.Println("Cat IDs:")
	for _, catID := range resp.CatIds {
		fmt.Printf("%d\n", catID)
	}

	if *showMetrics {
		printORCAMetrics(trailer)
	}
}

func listPhotosForCat(catID uint64) {
	client := getClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var trailer metadata.MD
	resp, err := client.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID}, grpc.Trailer(&trailer))
	if err != nil {
		log.Fatalf("ListPhotos failed: %v", err)
	}

	fmt.Printf("Photo IDs for cat %d:\n", catID)
	for _, photoID := range resp.PhotoIds {
		fmt.Printf("%d\n", photoID)
	}

	if *showMetrics {
		printORCAMetrics(trailer)
	}
}

func getCatPhoto(catID, photoID uint64) {
	client := getClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var trailer metadata.MD
	resp, err := client.GetPhoto(ctx, &pb.GetPhotoRequest{
		CatId:            catID,
		PhotoId:          photoID,
		Width:            uint32(*width),
		ScalingAlgorithm: getScalingAlgorithm(*algorithm),
	}, grpc.Trailer(&trailer))
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

	if *showMetrics {
		printORCAMetrics(trailer)
	}
}
