package httpfile

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"
)

// DisplayLimit is how much of a body the response tab shows; "Save to file" writes all of it.
const DisplayLimit = 5 << 20

// Body kinds the response tab highlights or describes.
const (
	KindJSON   = "json"
	KindXML    = "xml"
	KindHTML   = "html"
	KindText   = "text"
	KindImage  = "image"
	KindBinary = "binary"
	KindEmpty  = "empty"
)

// display is what the response tab shows of a body.
type display struct {
	contentType string // media type, declared or sniffed
	kind        string
	text        string // pretty-printed JSON, else the body as is; "" for binary
	truncated   bool
}

// formatBody detects the body's type and prepares its display text: JSON is
// pretty-printed (when it fits the display limit), anything is cut at limit.
func formatBody(raw []byte, contentType string, limit int) display {
	if len(raw) == 0 {
		return display{contentType: mediaType(contentType), kind: KindEmpty}
	}
	mt := mediaType(contentType)
	if mt == "" {
		mt = mediaType(http.DetectContentType(raw))
	}
	kind := kindOf(mt, raw)
	d := display{contentType: mt, kind: kind}
	if kind == KindImage || kind == KindBinary {
		return d
	}
	text := raw
	if kind == KindJSON && len(raw) <= limit {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err == nil {
			text = pretty.Bytes()
		}
	}
	if len(text) > limit {
		text, d.truncated = cutUTF8(text, limit), true
	}
	if !utf8.Valid(text) {
		return display{contentType: mt, kind: KindBinary}
	}
	d.text = string(text)
	return d
}

func mediaType(contentType string) string {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	return mt
}

func kindOf(mt string, raw []byte) string {
	switch {
	case mt == "application/json" || strings.HasSuffix(mt, "+json") || mt == "application/x-ndjson":
		return KindJSON
	case mt == "application/xml" || mt == "text/xml" || strings.HasSuffix(mt, "+xml"):
		return KindXML
	case mt == "text/html":
		return KindHTML
	case strings.HasPrefix(mt, "image/"):
		return KindImage
	case strings.HasPrefix(mt, "audio/"), strings.HasPrefix(mt, "video/"), mt == "application/pdf", mt == "application/zip":
		return KindBinary
	}
	// Plain text and unknown types: JSON when it parses, text when it's UTF-8.
	sample := raw[:min(len(raw), 512)]
	if trimmed := bytes.TrimSpace(sample); len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(raw) {
		return KindJSON
	}
	if utf8.Valid(cutUTF8(raw, min(len(raw), 64<<10))) {
		return KindText
	}
	return KindBinary
}

// cutUTF8 returns at most limit bytes of b without splitting a UTF-8 sequence.
func cutUTF8(b []byte, limit int) []byte {
	if len(b) <= limit {
		return b
	}
	end := limit
	for end > 0 && end > limit-utf8.UTFMax && !utf8.RuneStart(b[end]) {
		end--
	}
	return b[:end]
}

// decode undoes a Content-Encoding the transport left in place (when the request set
// Accept-Encoding itself). Unknown encodings (br, zstd) are returned as they came.
func decode(raw []byte, encoding string, limit int64) []byte {
	var r io.Reader
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return raw
		}
		r = zr
	case "deflate":
		// Servers mostly send zlib-wrapped deflate; some send it raw.
		if zr, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
			r = zr
		} else {
			r = flate.NewReader(bytes.NewReader(raw))
		}
	default:
		return raw
	}
	out, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return raw
	}
	return out
}
