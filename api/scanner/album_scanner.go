package scanner

import (
	"container/list"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/h2non/filetype"
	"github.com/viktorstrate/photoview/api/graphql/models"
	"github.com/viktorstrate/photoview/api/graphql/notification"
	"github.com/viktorstrate/photoview/api/utils"
)

type scanner_cache map[string]interface{}

func (cache *scanner_cache) insert_photo_type(path string, content_type string) {
	(*cache)["photo_type//"+path] = content_type
}

func (cache *scanner_cache) get_photo_type(path string) *string {
	result, found := (*cache)["photo_type//"+path].(string)
	if found {
		// log.Printf("Image cache hit: %s\n", path)
		return &result
	}

	return nil
}

// Insert single album directory in cache
func (cache *scanner_cache) insert_album_path(path string, contains_photo bool) {
	(*cache)["album_path//"+path] = contains_photo
}

// Insert album path and all parent directories up to the given root directory in cache
func (cache *scanner_cache) insert_album_paths(end_path string, root string, contains_photo bool) {
	curr_path := path.Clean(end_path)
	root_path := path.Clean(root)

	for curr_path != root_path || curr_path == "." {

		cache.insert_album_path(curr_path, contains_photo)

		curr_path = path.Dir(curr_path)
	}
}

func (cache *scanner_cache) album_contains_photo(path string) *bool {
	contains_photo, found := (*cache)["album_path//"+path].(bool)
	if found {
		// log.Printf("Album cache hit: %s\n", path)
		return &contains_photo
	}

	return nil
}

func ScanUser(database *sql.DB, userId int) error {

	row := database.QueryRow("SELECT * FROM user WHERE user_id = ?", userId)
	user, err := models.NewUserFromRow(row)
	if err != nil {
		log.Printf("Could not find user to scan: %s\n", err.Error())
		return err
	}

	log.Printf("Starting scan for user '%s'\n", user.Username)
	go scan(database, user)

	return nil
}

func scan(database *sql.DB, user *models.User) {

	notifyKey := utils.GenerateToken()
	processKey := utils.GenerateToken()

	timeout := 3000
	notification.BroadcastNotification(&models.Notification{
		Key:     notifyKey,
		Type:    models.NotificationTypeMessage,
		Header:  "User scan started",
		Content: "Scanning has started...",
		Timeout: &timeout,
	})

	// Start scanning
	scanner_cache := make(scanner_cache)
	album_paths_scanned := make([]interface{}, 0)

	type scanInfo struct {
		path     string
		parentId *int
	}

	scanQueue := list.New()
	scanQueue.PushBack(scanInfo{
		path:     user.RootPath,
		parentId: nil,
	})

	newPhotos := list.New()

	for scanQueue.Front() != nil {
		albumInfo := scanQueue.Front().Value.(scanInfo)
		scanQueue.Remove(scanQueue.Front())

		albumPath := albumInfo.path
		albumParentId := albumInfo.parentId

		album_paths_scanned = append(album_paths_scanned, albumPath)

		// Read path
		dirContent, err := ioutil.ReadDir(albumPath)
		if err != nil {
			ScannerError("Could not read directory: %s\n", err.Error())
			continue
		}

		tx, err := database.Begin()
		if err != nil {
			ScannerError("Could not begin database transaction: %s\n", err)
			continue
		}

		log.Printf("Scanning directory: %s", albumPath)

		// Make album if not exists
		albumTitle := path.Base(albumPath)
		_, err = tx.Exec("INSERT IGNORE INTO album (title, parent_album, owner_id, path) VALUES (?, ?, ?, ?)", albumTitle, albumParentId, user.UserID, albumPath)
		if err != nil {
			ScannerError("Could not insert album into database: %s\n", err)
			tx.Rollback()
			continue
		}

		row := tx.QueryRow("SELECT album_id FROM album WHERE path = ?", albumPath)
		var albumId int
		if err := row.Scan(&albumId); err != nil {
			ScannerError("Could not get id of album: %s\n", err)
			tx.Rollback()
			return
		}

		// Commit album transaction
		if err := tx.Commit(); err != nil {
			log.Printf("ERROR: Could not commit database transaction: %s\n", err)
			return
		}

		// Scan for photos
		for _, item := range dirContent {
			photoPath := path.Join(albumPath, item.Name())

			if !item.IsDir() && isPathImage(photoPath, &scanner_cache) {
				tx, err := database.Begin()
				if err != nil {
					ScannerError("Could not begin database transaction for image %s: %s\n", photoPath, err)
					continue
				}

				photo, newPhoto, err := ScanPhoto(tx, photoPath, albumId, processKey)
				if err != nil {
					ScannerError("Scanning image %s: %s", photoPath, err)
					tx.Rollback()
					continue
				}

				if newPhoto {
					newPhotos.PushBack(photo)

					if newPhotos.Len()%25 == 0 {
						notification.BroadcastNotification(&models.Notification{
							Key:     processKey,
							Type:    models.NotificationTypeMessage,
							Header:  "Scanning photo",
							Content: fmt.Sprintf("Scanning image at %s", photoPath),
						})
					}
				}

				tx.Commit()
			}
		}

		// Scan for sub-albums
		for _, item := range dirContent {
			subalbumPath := path.Join(albumPath, item.Name())

			if item.IsDir() && directoryContainsPhotos(subalbumPath, &scanner_cache) {
				scanQueue.PushBack(scanInfo{
					path:     subalbumPath,
					parentId: &albumId,
				})
			}
		}
	}

	completeMessage := "No new photos were found"
	if newPhotos.Len() > 0 {
		completeMessage = fmt.Sprintf("%d new photos were found", newPhotos.Len())
	}

	notification.BroadcastNotification(&models.Notification{
		Key:      notifyKey,
		Type:     models.NotificationTypeMessage,
		Header:   "Scan completed",
		Content:  completeMessage,
		Positive: true,
	})

	cleanupCache(database, album_paths_scanned, user)

	err := processUnprocessedPhotos(database, user, notifyKey)
	if err != nil {
		log.Printf("ERROR: processing photos: %s\n", err)
	}

	log.Println("Done scanning")
}

func directoryContainsPhotos(rootPath string, cache *scanner_cache) bool {

	if contains_image := cache.album_contains_photo(rootPath); contains_image != nil {
		return *contains_image
	}

	scanQueue := list.New()
	scanQueue.PushBack(rootPath)

	scanned_directories := make([]string, 0)

	for scanQueue.Front() != nil {

		dirPath := scanQueue.Front().Value.(string)
		scanQueue.Remove(scanQueue.Front())

		scanned_directories = append(scanned_directories, dirPath)

		dirContent, err := ioutil.ReadDir(dirPath)
		if err != nil {
			ScannerError("Could not read directory: %s\n", err.Error())
			return false
		}

		for _, fileInfo := range dirContent {
			filePath := path.Join(dirPath, fileInfo.Name())
			if fileInfo.IsDir() {
				scanQueue.PushBack(filePath)
			} else {
				if isPathImage(filePath, cache) {
					cache.insert_album_paths(dirPath, rootPath, true)
					return true
				}
			}
		}

	}

	for _, scanned_path := range scanned_directories {
		cache.insert_album_path(scanned_path, false)
	}
	return false
}

var SupportedMimetypes = [...]string{
	"image/jpeg",
	"image/png",
	"image/tiff",
	"image/webp",
	"image/x-canon-cr2",
	"image/bmp",
}

var WebMimetypes = [...]string{
	"image/jpeg",
	"image/png",
	"image/webp",
	"image/bmp",
}

func isPathImage(path string, cache *scanner_cache) bool {
	if cache.get_photo_type(path) != nil {
		return true
	}
	file, err := os.Open(path)
	if err != nil {
		ScannerError("Could not open file %s: %s\n", path, err)
		return false
	}
	defer file.Close()

	head := make([]byte, 261)
	if _, err := file.Read(head); err != nil {
		ScannerError("Could not read file %s: %s\n", path, err)
		return false
	}

	imgType, err := filetype.Image(head)
	if err != nil {
		return false
	}

	for _, supported_mime := range SupportedMimetypes {
		if supported_mime == imgType.MIME.Value {
			cache.insert_photo_type(path, supported_mime)
			return true
		}
	}

	log.Printf("Unsupported image %s of type %s\n", path, imgType.MIME.Value)
	return false
}

func processUnprocessedPhotos(database *sql.DB, user *models.User, notifyKey string) error {

	processKey := utils.GenerateToken()

	rows, err := database.Query(`
		SELECT photo.* FROM photo JOIN album ON photo.album_id = album.album_id
		WHERE album.owner_id = ?
		AND photo.photo_id NOT IN (
			SELECT photo_id FROM photo_url WHERE photo_url.photo_id = photo.photo_id
		)
	`, user.UserID)
	if err != nil {
		ScannerError("Could not get photos to process from db")
		return err
	}

	photosToProcess, err := models.NewPhotosFromRows(rows)
	if err != nil {
		if err == sql.ErrNoRows {
			// No photos to process
			return nil
		}

		ScannerError("Could not parse photos to process from db %s", err)
		return err
	}

	// Proccess all photos
	for count, photo := range photosToProcess {

		tx, err := database.Begin()
		if err != nil {
			ScannerError("Could not start database transaction: %s", err)
			continue
		}

		var progress float64 = float64(count) / float64(len(photosToProcess)) * 100.0

		notification.BroadcastNotification(&models.Notification{
			Key:      processKey,
			Type:     models.NotificationTypeProgress,
			Header:   fmt.Sprintf("Processing photos (%d of %d)", count, len(photosToProcess)),
			Content:  fmt.Sprintf("Processing photo at %s", photo.Path),
			Progress: &progress,
		})

		err = ProcessPhoto(tx, photo)
		if err != nil {
			tx.Rollback()
			ScannerError("Could not process photo: %s", err)
			continue
		}

		err = tx.Commit()
		if err != nil {
			ScannerError("Could not commit db transaction: %s", err)
			continue
		}
	}

	if len(photosToProcess) > 0 {
		notification.BroadcastNotification(&models.Notification{
			Key:      notifyKey,
			Type:     models.NotificationTypeMessage,
			Header:   "Processing completed",
			Content:  fmt.Sprintf("%d photos have been processed", len(photosToProcess)),
			Positive: true,
		})

		notification.BroadcastNotification(&models.Notification{
			Key:  processKey,
			Type: models.NotificationTypeClose,
		})
	}

	return nil
}

func cleanupCache(database *sql.DB, scanned_albums []interface{}, user *models.User) {
	if len(scanned_albums) == 0 {
		return
	}

	args := make([]interface{}, 0)
	args = append(args, user.UserID)
	args = append(args, scanned_albums...)

	albums_questions := strings.Repeat("?,", len(scanned_albums))[:len(scanned_albums)*2-1]
	rows, err := database.Query("SELECT album_id FROM album WHERE album.owner_id = ? AND path NOT IN ("+albums_questions+")", args...)
	if err != nil {
		ScannerError("Could not get albums from database: %s\n", err)
		return
	}
	defer rows.Close()

	deleted_albums := 0
	deleted_ids := make([]interface{}, 0)
	for rows.Next() {
		var album_id int
		if err := rows.Scan(&album_id); err != nil {
			ScannerError("Could not parse album to be removed (album_id %d): %s\n", album_id, err)
		}

		deleted_ids = append(deleted_ids, album_id)
		deleted_albums++
		cache_path := path.Join("./photo_cache", strconv.Itoa(album_id))
		err := os.RemoveAll(cache_path)
		if err != nil {
			ScannerError("Could not delete unused cache folder: %s\n%s\n", cache_path, err)
		}
	}

	if len(deleted_ids) > 0 {
		albums_questions = strings.Repeat("?,", len(deleted_ids))[:len(deleted_ids)*2-1]

		if _, err := database.Exec("DELETE FROM album WHERE album_id IN ("+albums_questions+")", deleted_ids...); err != nil {
			ScannerError("Could not delete old albums from database:\n%s\n", err)
		}

		notification.BroadcastNotification(&models.Notification{
			Key:     utils.GenerateToken(),
			Type:    models.NotificationTypeMessage,
			Header:  "Deleted old albums",
			Content: fmt.Sprintf("Deleted %d albums, that was not found", len(deleted_ids)),
		})
	}

	log.Printf("Deleted %d unused albums from cache", deleted_albums)
}

func ScannerError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)

	log.Printf("ERROR: %s", message)
	notification.BroadcastNotification(&models.Notification{
		Key:      utils.GenerateToken(),
		Type:     models.NotificationTypeMessage,
		Header:   "Scanner error",
		Content:  message,
		Negative: true,
	})
}

func PhotoCache() string {
	photoCache := os.Getenv("PHOTO_CACHE")
	if photoCache == "" {
		photoCache = "./photo_cache"
	}

	return photoCache
}
