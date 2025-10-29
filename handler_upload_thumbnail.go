package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

const maxMemory = 10 << 20 // 10 MB

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse multipart form", err)
		return
	}
	file, fh, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get file", err)
		return

	}
	defer func(file multipart.File) {
		err := file.Close()
		if err != nil {
			fmt.Println("Couldn't close file", err)
		}
	}(file)

	mediaType := fh.Header.Get("Content-Type")
	byteSlice, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't read file", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You can't upload a thumbnail for this video", err)
		return
	}

	videoThumbnails[videoID] = thumbnail{
		data:      byteSlice,
		mediaType: mediaType,
	}

	fileExt := mediaType[6:]

	thumbnailPath := filepath.Join(cfg.assetsRoot, fmt.Sprintf("%s.%s", makeRandName(), fileExt))
	thumbnailURL := fmt.Sprintf("http://localhost:%s/%s", cfg.port, thumbnailPath)
	video.ThumbnailURL = &thumbnailURL

	create, err := os.Create(thumbnailPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create thumbnail file", err)
		return
	}

	// as we've already read the file, we need to reset the file pointer before copying
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue reading the file", err)
		return
	}

	if _, err := io.Copy(create, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't write thumbnail file", err)
		return
	}

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}
	respondWithJSON(w, http.StatusOK, struct{}{})
}

func makeRandName() string {
	var destBytes = make([]byte, base64.RawURLEncoding.EncodedLen(32))
	var randBytes = make([]byte, 32)
	_, err := rand.Read(randBytes)
	if err != nil {
		fmt.Println("Couldn't generate random bytes", err)
		return ""
	}
	base64.RawURLEncoding.Encode(destBytes, randBytes)
	return string(destBytes)
}
