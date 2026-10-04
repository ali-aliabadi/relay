package message

import (
	"bytes"
	"encoding/base64"
	"mime"
	"strings"
	"unicode"
)

var magic = map[string][]byte{
	"image/png":  []byte("\x89PNG\r\n\x1a\n"),
	"image/jpeg": {0xFF, 0xD8, 0xFF},
}

// DefaultFileContentType is used for a file block without content_type.
const DefaultFileContentType = "application/octet-stream"

func checkImage(c *checker, path string, b *Block) (Attachment, bool) {
	c.maxLen(path+".caption", b.Caption, MaxCaptionLen)
	switch {
	case b.URL != "" && b.Base64 != "":
		c.addf("%s: use either url or base64, not both", path)
	case b.URL != "":
		checkURL(c, path+".url", b.URL)
		if b.ContentType != "" {
			c.addf("%s.content_type: only used with base64", path)
		}
	case b.Base64 != "":
		return decodeImage(c, path, b)
	default:
		c.addf("%s: needs url or base64", path)
	}
	return Attachment{}, false
}

func decodeImage(c *checker, path string, b *Block) (Attachment, bool) {
	sig, ok := magic[b.ContentType]
	if !ok {
		c.addf("%s.content_type: must be image/png or image/jpeg", path)
		return Attachment{}, false
	}
	data, ok := decodeBase64(c, path+".base64", b.Base64, MaxImageBytes, "image")
	if !ok {
		return Attachment{}, false
	}
	if !bytes.HasPrefix(data, sig) {
		c.addf("%s.base64: content is not a %s image", path, b.ContentType)
		return Attachment{}, false
	}
	b.Base64 = ""
	return Attachment{ContentType: b.ContentType, Bytes: data}, true
}

// checkFile validates a file block. Files are inline only: Relay fetches no
// URLs, and Telegram only fetches a few file types by URL.
func checkFile(c *checker, path string, b *Block) (Attachment, bool) {
	c.maxLen(path+".caption", b.Caption, MaxCaptionLen)
	checkFilename(c, path+".filename", b.Filename)
	checkFileContentType(c, path+".content_type", b)
	if b.Base64 == "" {
		c.addf("%s.base64: required", path)
		return Attachment{}, false
	}
	data, ok := decodeBase64(c, path+".base64", b.Base64, MaxFileBytes, "file")
	if !ok {
		return Attachment{}, false
	}
	if len(data) == 0 {
		c.addf("%s.base64: file is empty", path)
		return Attachment{}, false
	}
	b.Base64 = ""
	return Attachment{ContentType: b.ContentType, Bytes: data}, true
}

// checkFileContentType defaults and normalizes content_type, so only a
// well-formed media type reaches a provider's upload headers.
func checkFileContentType(c *checker, path string, b *Block) {
	if b.ContentType == "" {
		b.ContentType = DefaultFileContentType
		return
	}
	mt, params, err := mime.ParseMediaType(b.ContentType)
	if err != nil || !strings.Contains(mt, "/") || len(b.ContentType) > MaxContentTypeLen {
		c.addf("%s: must be a media type like application/pdf, up to %d characters", path, MaxContentTypeLen)
		return
	}
	b.ContentType = mime.FormatMediaType(mt, params)
}

// checkFilename allows a plain name only: no directories, no control
// characters, so it is safe in a multipart header and on the recipient's disk.
func checkFilename(c *checker, path, name string) {
	c.required(path, name, MaxFilenameLen)
	bad := strings.ContainsAny(name, `/\`) || strings.ContainsFunc(name, unicode.IsControl) ||
		strings.Trim(name, ". ") == ""
	if name != "" && bad {
		c.addf("%s: must be a plain file name without slashes or control characters", path)
	}
}

func decodeBase64(c *checker, path, raw string, limit int, what string) ([]byte, bool) {
	if base64.StdEncoding.DecodedLen(len(raw)) > limit+3 {
		c.addf("%s: %s larger than %d bytes", path, what, limit)
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		c.addf("%s: not valid standard base64", path)
		return nil, false
	}
	if len(data) > limit {
		c.addf("%s: %s larger than %d bytes", path, what, limit)
		return nil, false
	}
	return data, true
}
