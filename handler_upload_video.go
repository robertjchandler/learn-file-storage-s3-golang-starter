package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	const maxMemory = 1 << 30 // 1 GB
	var c io.ReadCloser
	http.MaxBytesReader(w, c, maxMemory)

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}
	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to upload this video", nil)
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse media type", nil)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusForbidden, "Incorrect media type", nil)
		return
	}

	temp, err := os.CreateTemp("", "tubely-upload.mp4")
	defer os.Remove("tubely-upload.mp4")
	defer temp.Close()

	io.Copy(temp, file)
	temp.Seek(0, io.SeekStart)

	key := make([]byte, 32)
	rand.Read(key)
	keyString := base64.URLEncoding.EncodeToString(key) + mediaTypeToExt(mediaType)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = cfg.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &keyString,
		Body:        temp,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video", err)
		return
	}

	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, keyString)
	video.VideoURL = &url
	cfg.db.UpdateVideo(video)
}
