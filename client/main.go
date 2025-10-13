package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"time"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

var (
	listCats     = flag.Bool("list-cats", false, "List all cat IDs")
	listPhotos   = flag.Uint64("list-photos", 0, "List photo IDs for cat ID")
	catID        = flag.Uint64("cat-id", 0, "Cat ID for get-photo")
	photoID      = flag.Uint64("photo-id", 0, "Photo ID for get-photo")
	outputFile   = flag.String("output", "", "Output file for photo data")
	serverAddr   = flag.String("addr", "localhost:8081", "Server address")
	showMetrics  = flag.Bool("show-metrics", false, "Show ORCA metrics from trailers")
	width        = flag.Uint("width", 0, "Width for scaling (0 = no scaling)")
	algorithm    = flag.String("algorithm", "BILINEAR", "Scaling algorithm: NEAREST_NEIGHBOR, BILINEAR, CATMULL_ROM, APPROX_BILINEAR")
	streamPhotos = flag.String("stream-photos", "", "Stream multiple photos (format: cat_id1:photo_id1,cat_id2:photo_id2,...)")
	outputDir    = flag.String("output-dir", "/tmp", "Output directory for photos")
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
		return pb.ScalingAlgorithm_NONE
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

	if *streamPhotos != "" {
		getPhotosStream(*streamPhotos)
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

func saveFile(catId, photoId uint64, data []byte) {
	filename := fmt.Sprintf("%s/cat_%d_photo_%d.jpg", *outputDir, catId, photoId)
	err := ioutil.WriteFile(filename, data, 0644)
	if err != nil {
		log.Printf("Failed to write file %s: %v", filename, err)
	} else {
		fmt.Printf("Cat %d, Photo %d saved to %s (%d bytes)\n",
			catId, photoId, filename, len(data))
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

	saveFile(catID, photoID, resp.PhotoData)

	if *showMetrics {
		printORCAMetrics(trailer)
	}
}

func parsePhotoRequests(input string) ([]*pb.PhotoRequest, error) {
	pairs := strings.Split(input, ",")
	var requests []*pb.PhotoRequest

	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format for pair: %s (expected cat_id:photo_id)", pair)
		}

		catID, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cat_id: %s", parts[0])
		}

		photoID, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid photo_id: %s", parts[1])
		}

		requests = append(requests, &pb.PhotoRequest{
			CatId:   catID,
			PhotoId: photoID,
		})
	}

	return requests, nil
}

func getPhotosStream(photoRequestsStr string) {
	client := getClient()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Parse photo requests
	photoRequests, err := parsePhotoRequests(photoRequestsStr)
	if err != nil {
		log.Fatalf("Failed to parse photo requests: %v", err)
	}

	// Create stream request
	req := &pb.GetPhotosStreamRequest{
		PhotoRequests:    photoRequests,
		Width:            uint32(*width),
		ScalingAlgorithm: getScalingAlgorithm(*algorithm),
	}

	// Start streaming
	var trailer metadata.MD
	stream, err := client.GetPhotosStream(ctx, req, grpc.Trailer(&trailer))
	if err != nil {
		log.Fatalf("Failed to start streaming: %v", err)
	}

	err = stream.CloseSend()
	if err != nil {
		log.Fatalf("Close send error: %v", err)
	}

	fmt.Printf("Streaming %d photos...\n", len(photoRequests))

	for {
		response, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Failed to receive response: %v", err)
		}

		if response.Success {
			saveFile(response.CatId, response.PhotoId, response.PhotoData)
		} else {
			fmt.Printf("Error Cat %d, Photo %d: %s\n",
				response.CatId, response.PhotoId, response.ErrorMessage)
		}
	}

	fmt.Println("Streaming completed.")
	if *showMetrics {
		printORCAMetrics(trailer)
	}
}
