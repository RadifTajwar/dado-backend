package handlers

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"dado/internal/config"
)

const maxUpload = 10 << 20 // 10 MB

var uploadClient = &http.Client{Timeout: 60 * time.Second}

type uploadResult struct {
	URL      string `json:"url"`
	PublicID string `json:"publicId"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// POST /api/admin/uploads — multipart field "file". Stores the image on
// Cloudinary (folder "dado") and returns its https URL.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.UploadsEnabled() {
		writeError(w, http.StatusServiceUnavailable, "Image uploads are not set up yet. Add the Cloudinary keys to backend/.env.")
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "multipart/form-data" {
		writeError(w, http.StatusBadRequest, "Send the image as multipart/form-data.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<20) // 1 MB of room for the multipart headers
	file, header, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "Images must be 10 MB or smaller.")
		} else {
			writeInvalid(w, fieldErrors{"file": "Choose an image to upload."})
		}
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil {
		writeServerError(w, r, err)
		return
	}
	if len(data) > maxUpload {
		writeError(w, http.StatusRequestEntityTooLarge, "Images must be 10 MB or smaller.")
		return
	}
	if imageType(data) == "" { // judged by the bytes, not the file name
		writeInvalid(w, fieldErrors{"file": "Upload a JPG, PNG, WebP, AVIF or GIF image."})
		return
	}

	res, err := uploadToCloudinary(r.Context(), h.cfg, header.Filename, data)
	if err != nil {
		slog.Error("cloudinary upload failed", "err", err)
		writeError(w, http.StatusBadGateway, "The upload failed. Try again in a minute.")
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// imageType sniffs the first bytes; "" means not an image we accept.
func imageType(data []byte) string {
	// Go's sniffer doesn't know AVIF: an ISO box "ftyp" with brand avif/avis.
	if len(data) >= 12 && string(data[4:8]) == "ftyp" && (string(data[8:12]) == "avif" || string(data[8:12]) == "avis") {
		return "image/avif"
	}
	switch t := http.DetectContentType(data); t {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return t
	}
	return ""
}

// uploadToCloudinary does a signed upload through the REST API. The signature
// is the SHA-1 of the sorted parameters followed by the API secret.
func uploadToCloudinary(ctx context.Context, cfg *config.Config, filename string, data []byte) (*uploadResult, error) {
	const folder = "dado"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha1.Sum([]byte("folder=" + folder + "&timestamp=" + ts + cfg.CloudinarySecret))

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range map[string]string{
		"api_key":   cfg.CloudinaryKey,
		"timestamp": ts,
		"folder":    folder,
		"signature": hex.EncodeToString(sum[:]),
	} {
		if err := mw.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	endpoint := "https://api.cloudinary.com/v1_1/" + url.PathEscape(cfg.CloudinaryCloud) + "/image/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := uploadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var out struct {
		SecureURL string `json:"secure_url"`
		PublicID  string `json:"public_id"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Error     struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("cloudinary answered %d with unreadable JSON: %w", res.StatusCode, err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloudinary answered %d: %s", res.StatusCode, out.Error.Message)
	}
	return &uploadResult{URL: out.SecureURL, PublicID: out.PublicID, Width: out.Width, Height: out.Height}, nil
}
