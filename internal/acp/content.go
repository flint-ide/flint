package acp

import (
	"encoding/json"
	"fmt"
)

// Content block types (ContentBlock.type in the schema).
const (
	ContentText         = "text"
	ContentImage        = "image"
	ContentAudio        = "audio"
	ContentResourceLink = "resource_link"
	ContentResource     = "resource"
)

// ContentBlock is one piece of prompt or message content. Which fields are
// meaningful depends on Type:
//
//   - "text": Text
//   - "image": Data (base64), MimeType, optional URI
//   - "audio": Data (base64), MimeType
//   - "resource_link": URI, Name, optional MimeType/Title/Description/Size
//   - "resource": Resource (embedded text or blob contents)
//
// Agents always accept text and resource_link; image, audio and resource
// require the matching PromptCapabilities flag.
type ContentBlock struct {
	Type        string            `json:"type"`
	Text        string            `json:"text,omitempty"`
	Data        string            `json:"data,omitempty"`
	MimeType    string            `json:"mimeType,omitempty"`
	URI         string            `json:"uri,omitempty"`
	Name        string            `json:"name,omitempty"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Size        *int64            `json:"size,omitempty"`
	Resource    *ResourceContents `json:"resource,omitempty"`
	Annotations json.RawMessage   `json:"annotations,omitempty"`
}

// ResourceContents is the body of an embedded resource: text when Blob is
// empty, otherwise base64 binary data.
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// TextBlock returns a "text" content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentText, Text: text}
}

// ResourceLinkBlock returns a "resource_link" block referencing uri.
func ResourceLinkBlock(uri, name string) ContentBlock {
	return ContentBlock{Type: ContentResourceLink, URI: uri, Name: name}
}

// TextResourceBlock returns a "resource" block embedding text (for example a
// file's contents) under uri. Requires PromptCapabilities.EmbeddedContext.
func TextResourceBlock(uri, mimeType, text string) ContentBlock {
	return ContentBlock{
		Type:     ContentResource,
		Resource: &ResourceContents{URI: uri, MimeType: mimeType, Text: text},
	}
}

// MarshalJSON emits exactly the fields the schema defines for the block's
// type, including required fields whose value is empty (e.g. text: "").
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	out := map[string]any{"type": b.Type}
	if len(b.Annotations) > 0 {
		out["annotations"] = b.Annotations
	}
	switch b.Type {
	case ContentText:
		out["text"] = b.Text
	case ContentImage, ContentAudio:
		out["data"] = b.Data
		out["mimeType"] = b.MimeType
		if b.Type == ContentImage && b.URI != "" {
			out["uri"] = b.URI
		}
	case ContentResourceLink:
		out["uri"] = b.URI
		out["name"] = b.Name
		putIfSet(out, "mimeType", b.MimeType)
		putIfSet(out, "title", b.Title)
		putIfSet(out, "description", b.Description)
		if b.Size != nil {
			out["size"] = *b.Size
		}
	case ContentResource:
		if b.Resource == nil {
			return nil, fmt.Errorf("acp: resource content block without resource")
		}
		out["resource"] = b.Resource
	default:
		return nil, fmt.Errorf("acp: unsupported content block type %q", b.Type)
	}
	return json.Marshal(out)
}

// MarshalJSON emits "text" or "blob" (never both), keeping an empty text.
func (r ResourceContents) MarshalJSON() ([]byte, error) {
	out := map[string]any{"uri": r.URI}
	putIfSet(out, "mimeType", r.MimeType)
	if r.Blob != "" {
		out["blob"] = r.Blob
	} else {
		out["text"] = r.Text
	}
	return json.Marshal(out)
}

func putIfSet(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}
