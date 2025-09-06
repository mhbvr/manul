package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	pb "github.com/mhbvr/manul/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	serverAddr = flag.String("server", "localhost:8081", "gRPC server address")
	webPort    = flag.Int("port", 8080, "Web server port")
)

type WebServer struct {
	grpcClient pb.CatPhotosServiceClient
	grpcConn   *grpc.ClientConn
	templates  *template.Template
}

type PageData struct {
	Title   string
	Message string
	Error   string
}

type CatsPageData struct {
	PageData
	Cats []uint64
}

type PhotosPageData struct {
	PageData
	CatID  uint64
	Photos []uint64
}

type PhotoFullViewData struct {
	PageData
	CatID   uint64
	PhotoID uint64
}

func NewWebServer(serverAddr string) (*WebServer, error) {
	// Connect to gRPC server
	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %v", err)
	}

	client := pb.NewCatPhotosServiceClient(conn)

	// Load templates
	templates, err := template.ParseGlob("templates/*.html")
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to parse templates: %v", err)
	}

	return &WebServer{
		grpcClient: client,
		grpcConn:   conn,
		templates:  templates,
	}, nil
}

func (ws *WebServer) Close() error {
	return ws.grpcConn.Close()
}

func (ws *WebServer) handleHome(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:   "Cat Photo Storage",
		Message: "Welcome to the Cat Photo Storage System",
	}

	if err := ws.templates.ExecuteTemplate(w, "home.html", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ws *WebServer) handleCats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ws.grpcClient.ListCats(ctx, &pb.ListCatsRequest{})
	if err != nil {
		data := CatsPageData{
			PageData: PageData{
				Title: "All Cats",
				Error: fmt.Sprintf("Failed to get cats: %v", err),
			},
		}
		ws.templates.ExecuteTemplate(w, "cats.html", data)
		return
	}

	data := CatsPageData{
		PageData: PageData{
			Title: "All Cats",
		},
		Cats: resp.CatIds,
	}

	if err := ws.templates.ExecuteTemplate(w, "cats.html", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ws *WebServer) handlePhotos(w http.ResponseWriter, r *http.Request) {
	catIDStr := r.URL.Query().Get("cat_id")
	if catIDStr == "" {
		http.Error(w, "Missing cat_id parameter", http.StatusBadRequest)
		return
	}

	catID, err := strconv.ParseUint(catIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid cat_id parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ws.grpcClient.ListPhotos(ctx, &pb.ListPhotosRequest{CatId: catID})
	if err != nil {
		data := PhotosPageData{
			PageData: PageData{
				Title: fmt.Sprintf("Photos for Cat %d", catID),
				Error: fmt.Sprintf("Failed to get photos: %v", err),
			},
			CatID: catID,
		}
		ws.templates.ExecuteTemplate(w, "photos.html", data)
		return
	}

	data := PhotosPageData{
		PageData: PageData{
			Title: fmt.Sprintf("Photos for Cat %d", catID),
		},
		CatID:  catID,
		Photos: resp.PhotoIds,
	}

	if err := ws.templates.ExecuteTemplate(w, "photos.html", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ws *WebServer) handlePhoto(w http.ResponseWriter, r *http.Request) {
	catIDStr := r.URL.Query().Get("cat_id")
	photoIDStr := r.URL.Query().Get("photo_id")
	displayMode := r.URL.Query().Get("mode") // "thumb", "full", or empty (download)

	if catIDStr == "" || photoIDStr == "" {
		http.Error(w, "Missing cat_id or photo_id parameter", http.StatusBadRequest)
		return
	}

	catID, err := strconv.ParseUint(catIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid cat_id parameter", http.StatusBadRequest)
		return
	}

	photoID, err := strconv.ParseUint(photoIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid photo_id parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := ws.grpcClient.GetPhoto(ctx, &pb.GetPhotoRequest{
		CatId:   catID,
		PhotoId: photoID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get photo: %v", err), http.StatusNotFound)
		return
	}

	switch displayMode {
	case "thumb":
		// Display as thumbnail (inline image)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=3600")
	case "full":
		// Display full-size image inline
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=3600")
	default:
		// Download mode (original behavior)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"cat_%d_photo_%d.jpg\"", catID, photoID))
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(resp.PhotoData)))

	// Write photo data
	if _, err := w.Write(resp.PhotoData); err != nil {
		log.Printf("Error writing photo data: %v", err)
	}
}

func (ws *WebServer) handleFullPhoto(w http.ResponseWriter, r *http.Request) {
	catIDStr := r.URL.Query().Get("cat_id")
	photoIDStr := r.URL.Query().Get("photo_id")

	if catIDStr == "" || photoIDStr == "" {
		http.Error(w, "Missing cat_id or photo_id parameter", http.StatusBadRequest)
		return
	}

	catID, err := strconv.ParseUint(catIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid cat_id parameter", http.StatusBadRequest)
		return
	}

	photoID, err := strconv.ParseUint(photoIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid photo_id parameter", http.StatusBadRequest)
		return
	}

	data := PhotoFullViewData{
		PageData: PageData{
			Title: fmt.Sprintf("Cat %d - Photo %d", catID, photoID),
		},
		CatID:   catID,
		PhotoID: photoID,
	}

	if err := ws.templates.ExecuteTemplate(w, "photo_full.html", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func main() {
	flag.Parse()

	webServer, err := NewWebServer(*serverAddr)
	if err != nil {
		log.Fatalf("Failed to create web server: %v", err)
	}
	defer webServer.Close()

	// Setup routes
	http.HandleFunc("/", webServer.handleHome)
	http.HandleFunc("/cats", webServer.handleCats)
	http.HandleFunc("/photos", webServer.handlePhotos)
	http.HandleFunc("/photo", webServer.handlePhoto)
	http.HandleFunc("/view", webServer.handleFullPhoto)

	addr := fmt.Sprintf(":%d", *webPort)
	log.Printf("Web server starting on http://localhost%s", addr)
	log.Printf("Connecting to gRPC server at %s", *serverAddr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Web server failed: %v", err)
	}
}