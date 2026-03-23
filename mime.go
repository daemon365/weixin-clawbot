package weixin

import (
	"net/url"
	"path/filepath"
	"strings"
)

var extensionToMIME = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".txt":  "text/plain",
	".csv":  "text/csv",
	".zip":  "application/zip",
	".tar":  "application/x-tar",
	".gz":   "application/gzip",
	".mp3":  "audio/mpeg",
	".ogg":  "audio/ogg",
	".wav":  "audio/wav",
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

var mimeToExtension = map[string]string{
	"image/jpeg":        ".jpg",
	"image/jpg":         ".jpg",
	"image/png":         ".png",
	"image/gif":         ".gif",
	"image/webp":        ".webp",
	"image/bmp":         ".bmp",
	"video/mp4":         ".mp4",
	"video/quicktime":   ".mov",
	"video/webm":        ".webm",
	"video/x-matroska":  ".mkv",
	"video/x-msvideo":   ".avi",
	"audio/mpeg":        ".mp3",
	"audio/ogg":         ".ogg",
	"audio/wav":         ".wav",
	"application/pdf":   ".pdf",
	"application/zip":   ".zip",
	"application/x-tar": ".tar",
	"application/gzip":  ".gz",
	"text/plain":        ".txt",
	"text/csv":          ".csv",
}

func MIMEFromFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if mime, ok := extensionToMIME[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

func ExtensionFromMIME(mimeType string) string {
	ct := strings.TrimSpace(strings.ToLower(strings.SplitN(mimeType, ";", 2)[0]))
	if ext, ok := mimeToExtension[ct]; ok {
		return ext
	}
	return ".bin"
}

func ExtensionFromContentTypeOrURL(contentType, rawURL string) string {
	if contentType != "" {
		if ext := ExtensionFromMIME(contentType); ext != ".bin" {
			return ext
		}
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return ".bin"
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	if _, ok := extensionToMIME[ext]; ok {
		return ext
	}
	return ".bin"
}
