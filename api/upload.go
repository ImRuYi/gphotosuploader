// Package that contains the models of the JSON objects used in the requests and responses and the methods to create
// new objects that describes the API actions, like the Upload or the AtTokenScraper
package api

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"time"

	"log"

	"github.com/simonedegiacomi/gphotosuploader/auth"
)

var (
	RegexUploadedImageURL = regexp.MustCompile("^https:\\/\\/lh3\\.googleusercontent\\.com\\/([\\w-]+)$")
)

// UploadOptions contains the Upload options
type UploadOptions struct {
	// Required field, a stream from which read the image.
	// You need to close the stream when the image is uploaded
	Stream io.Reader

	// Required field, size of the photo
	FileSize int64

	// Name of the photo (optional)
	Name string

	// UNIX timestamp of the photo (optional)
	Timestamp int64

	// Optional album id
	AlbumId string

	// Optional album name
	AlbumName string
}

// NewUploadOptionsFromFile creates a new UploadOptions from a file
func NewUploadOptionsFromFile(file *os.File) (*UploadOptions, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("can't read file information (%v)", err)
	}

	return &UploadOptions{
		Stream:   file,
		FileSize: info.Size(),

		Name:      path.Base(file.Name()),
		Timestamp: info.ModTime().Unix() * 1000,
	}, nil
}

// Upload represents an upload, generated by an UploadOptions
type Upload struct {
	// Options of the upload
	Options *UploadOptions

	// Credentials to used to send the requests
	Credentials auth.CookieCredentials

	// URL to which send the request with the image (the real upload)
	url string

	// Id of the image got from the response of the request that enables the image
	idToMoveIntoAlbum string
}

// NewUpload creates a new Upload given an UploadOptions and a Credentials implementation. This method return an error if the
// upload options struct it's not usable to create a new upload
func NewUpload(options *UploadOptions, credentials auth.CookieCredentials) (*Upload, error) {
	if options.Stream == nil {
		return nil, errors.New("the stream of the UploadOptions is nil")
	}
	if options.FileSize <= 0 {
		return nil, errors.New("the fileSize of the UploadOptions is <= 0")
	}

	// Fill missing optional fields
	if options.Name == "" {
		options.Name = time.Now().String()
	}
	if options.Timestamp < 0 {
		options.Timestamp = time.Now().Unix()
	}

	return &Upload{
		Options:     options,
		Credentials: credentials,
	}, nil
}

func getImageIDFromURL(URL string) (string, error) {
	matches := RegexUploadedImageURL.FindStringSubmatch(URL)
	if len(matches) != 2 {
		return "", fmt.Errorf("url doesn't contain the image id")
	}
	return matches[1], nil
}

type UploadResult struct {
	Uploaded bool
	ImageID  string
	ImageUrl string
	AlbumID  string
}

func (ur *UploadResult) URLString() string {
	return fmt.Sprintf("https://lh3.googleusercontent.com/%s", ur.ImageID)
}

// Upload tries to upload an image, making multiple http requests. It returns a response event if there is an error
func (u *Upload) Upload() (*UploadResult, error) {
	// First request to get the upload url
	err := u.requestUploadURL()
	if err != nil {
		return &UploadResult{Uploaded: false}, errors.New("can't get an upload url")
	}

	// Upload the real image file
	token, err := u.uploadFile()
	if err != nil {
		return &UploadResult{Uploaded: false}, errors.New("can't upload file to the url obtained from the previously request")
	}

	// Enable the photo
	uploadedImageURL, err := u.enablePhoto(token)
	if err != nil {
		log.Println("[WARNING] The file has been uploaded, but the image url in the reply was not found. The image may not appear.")
		return &UploadResult{
			Uploaded: true,
		}, err
	}
	uploadedImageID, err := getImageIDFromURL(uploadedImageURL)
	if err != nil {
		log.Println("[WARNING] the file has been uploaded, but the image URL does not contain its id. The image may not appear.")
		return &UploadResult{
			Uploaded: true,
			ImageUrl: uploadedImageURL,
		}, err
	}

	// Add the image to an album if needed
	if u.Options.AlbumId != "" {
		u.moveToAlbum(u.Options.AlbumId)
	}

	createdAlbumID := ""
	// Create album and add the image if needed
	if u.Options.AlbumName != "" {
		createdAlbumID, err = u.createAlbum(u.Options.AlbumName)
		if err != nil {
			log.Println("[WARNING] the file has been uploaded, but the album hasn't been created.")
			return &UploadResult{
				Uploaded: true,
				ImageID:  uploadedImageID,
				ImageUrl: uploadedImageURL,
			}, err
		}
	}

	// No errors, image uploaded!
	return &UploadResult{
		Uploaded: true,
		ImageID:  uploadedImageID,
		ImageUrl: uploadedImageURL,
		AlbumID:  createdAlbumID,
	}, nil
}
