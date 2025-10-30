package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

const maxFileSize = 1 << 30 // 1 GB
const bucketUrl = "https://%s.s3.%s.amazonaws.com/%s"

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize)

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

	fmt.Println("uploading video", videoID, "by user", userID)

	err = r.ParseMultipartForm(maxFileSize)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse multipart form", err)
		return
	}
	file, fh, err := r.FormFile("video")
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
	parseMediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse media type", err)
		return
	}

	if parseMediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You can't upload a video for another user", err)
		return
	}

	byteSlice, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't read file", err)
		return
	}

	temp, err := os.CreateTemp(".", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
		return
	}
	defer func(temp *os.File) {
		err := temp.Close()
		if err != nil {
			fmt.Println("Couldn't close temp file", err)
		}
	}(temp)
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			fmt.Println("Couldn't remove temp file", err)
		}
	}(temp.Name())

	_, err = io.Copy(temp, bytes.NewReader(byteSlice))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't write file", err)
		return
	}

	// as we've already read the file, we need to reset the file pointer before copying
	_, err = temp.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "There was an issue reading the file", err)
		return
	}

	aspectRatio := "other"
	ratio, err := getVideoAspectRatio(temp.Name())
	if err == nil {
		switch ratio {
		case "16:9":
			aspectRatio = "landscape"
		case "9:16":
			aspectRatio = "portrait"
		default:
			aspectRatio = "other"
		}
	}

	bucketName := "tubely-04073108"
	videoKey := fmt.Sprintf("%s/%s.mp4", aspectRatio, makeRandName())
	fileUrl := fmt.Sprintf(bucketUrl, bucketName, "eu-central-1", videoKey)
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &bucketName,
		Key:         &videoKey,
		Body:        temp,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video to S3", err)
		return
	}

	video.VideoURL = &fileUrl
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, struct{}{})
}

func getVideoAspectRatio(filePath string) (string, error) {
	output, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath).Output()
	if err != nil {
		return "", err
	}

	var probeOutput FFProbeResponse
	err = json.Unmarshal(output, &probeOutput)
	if err != nil {
		return "", err
	}

	for _, stream := range probeOutput.Streams {
		aspectRatio := stream.DisplayAspectRatio

		if aspectRatio == "16:9" {
			return "16:9", nil
		} else if aspectRatio == "9:16" {
			return "9:16", nil
		} else {
			return "other", nil
		}
	}
	return "other", nil
}
