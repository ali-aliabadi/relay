package message

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// ValidationError lists every problem with a request by field path. Its
// messages never contain the caller's values, only paths and rules.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string { return strings.Join(e.Problems, "; ") }

// maxProblems caps how many problems one error reports.
const maxProblems = 10

type checker struct{ problems []string }

func (c *checker) addf(format string, args ...any) {
	if len(c.problems) < maxProblems {
		c.problems = append(c.problems, fmt.Sprintf(format, args...))
	}
}

func (c *checker) maxLen(path, s string, limit int) {
	if utf8.RuneCountInString(s) > limit {
		c.addf("%s: longer than %d characters", path, limit)
	}
}

func (c *checker) required(path, s string, limit int) {
	if strings.TrimSpace(s) == "" {
		c.addf("%s: required", path)
		return
	}
	c.maxLen(path, s, limit)
}

// Normalize applies defaults (urgency, the text shorthand), validates the
// request and moves inline image bytes out of the blocks. The returned
// request's image blocks reference the returned images by index.
func Normalize(req Request) (Request, []Image, error) {
	c := &checker{}
	if req.Urgency == "" {
		req.Urgency = UrgencyNormal
	}
	if req.Text != "" {
		if len(req.Blocks) > 0 {
			c.addf("text: use either text or blocks, not both")
		}
		req.Blocks = []Block{{Type: BlockText, Text: req.Text}}
		req.Text = ""
	}
	checkEnvelope(c, req)
	blocks := make([]Block, len(req.Blocks))
	copy(blocks, req.Blocks)
	var images []Image
	imageBlocks := 0
	for i := range blocks {
		if blocks[i].Type == BlockImage {
			imageBlocks++
		}
		if img, ok := checkBlock(c, fmt.Sprintf("blocks[%d]", i), &blocks[i]); ok {
			n := len(images)
			blocks[i].Attachment = &n
			images = append(images, img)
		}
	}
	if imageBlocks > MaxImages {
		c.addf("blocks: at most %d image", MaxImages)
	}
	if len(c.problems) > 0 {
		return Request{}, nil, &ValidationError{Problems: c.problems}
	}
	req.Blocks = blocks
	return req, images, nil
}

func checkEnvelope(c *checker, req Request) {
	switch req.Urgency {
	case UrgencyLow, UrgencyNormal, UrgencyHigh, UrgencyCritical:
	default:
		c.addf("urgency: must be low, normal, high or critical")
	}
	if len(req.To) == 0 || len(req.To) > MaxRecipients {
		c.addf("to: must list 1-%d recipients", MaxRecipients)
	}
	checkUnique(c, "to", req.To)
	if len(req.Channels) > 0 {
		checkUnique(c, "channels", req.Channels)
	}
	c.maxLen("title", req.Title, MaxTitleLen)
	c.maxLen("source", req.Source, MaxSourceLen)
	c.maxLen("idempotency_key", req.IdempotencyKey, MaxIdempotencyLen)
	if len(req.Blocks) == 0 {
		c.addf("blocks: send text or at least one block")
	}
	if len(req.Blocks) > MaxBlocks {
		c.addf("blocks: at most %d blocks", MaxBlocks)
	}
}

func checkUnique(c *checker, path string, values []string) {
	seen := map[string]bool{}
	for i, v := range values {
		if v == "" {
			c.addf("%s[%d]: required", path, i)
		} else if seen[v] {
			c.addf("%s[%d]: duplicate", path, i)
		}
		seen[v] = true
	}
}

// checkBlock validates one block. For an inline image it returns the decoded
// bytes and clears Base64 so the stored blocks don't hold them.
func checkBlock(c *checker, path string, b *Block) (Image, bool) {
	if b.Attachment != nil {
		c.addf("%s.attachment: set by Relay, not by callers", path)
	}
	if allowed, known := allowedFields[b.Type]; known && !onlyFields(b, allowed) {
		c.addf("%s: has fields that don't belong to a %q block", path, b.Type)
	}
	switch b.Type {
	case BlockText:
		c.required(path+".text", b.Text, MaxTextLen)
	case BlockCode:
		c.required(path+".text", b.Text, MaxTextLen)
	case BlockFields:
		checkFields(c, path, b.Items)
	case BlockTable:
		checkTable(c, path, b.Columns, b.Rows)
	case BlockLink:
		c.required(path+".text", b.Text, MaxLinkTextLen)
		checkURL(c, path+".url", b.URL)
	case BlockImage:
		return checkImage(c, path, b)
	default:
		c.addf("%s.type: must be one of text, fields, table, image, code, link", path)
	}
	return Image{}, false
}

func checkFields(c *checker, path string, items []Field) {
	if len(items) == 0 || len(items) > MaxFields {
		c.addf("%s.items: must have 1-%d items", path, MaxFields)
	}
	for i, it := range items {
		c.required(fmt.Sprintf("%s.items[%d].label", path, i), it.Label, MaxLabelLen)
		c.maxLen(fmt.Sprintf("%s.items[%d].value", path, i), it.Value, MaxValueLen)
	}
}

func checkTable(c *checker, path string, cols []string, rows [][]string) {
	if len(cols) == 0 || len(cols) > MaxTableColumns {
		c.addf("%s.columns: must have 1-%d columns", path, MaxTableColumns)
	}
	if len(rows) > MaxTableRows {
		c.addf("%s.rows: at most %d rows", path, MaxTableRows)
	}
	for i, col := range cols {
		c.maxLen(fmt.Sprintf("%s.columns[%d]", path, i), col, MaxCellLen)
	}
	for i, row := range rows {
		if len(row) != len(cols) {
			c.addf("%s.rows[%d]: must have %d cells, one per column", path, i, len(cols))
		}
		for j, cell := range row {
			c.maxLen(fmt.Sprintf("%s.rows[%d][%d]", path, i, j), cell, MaxCellLen)
		}
	}
}

func checkURL(c *checker, path, raw string) {
	if raw == "" {
		c.addf("%s: required", path)
		return
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > MaxURLLen || u.Scheme != "https" || u.Host == "" {
		c.addf("%s: must be an https URL up to %d characters", path, MaxURLLen)
	}
}

var magic = map[string][]byte{
	"image/png":  []byte("\x89PNG\r\n\x1a\n"),
	"image/jpeg": {0xFF, 0xD8, 0xFF},
}

func checkImage(c *checker, path string, b *Block) (Image, bool) {
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
	return Image{}, false
}

func decodeImage(c *checker, path string, b *Block) (Image, bool) {
	sig, ok := magic[b.ContentType]
	if !ok {
		c.addf("%s.content_type: must be image/png or image/jpeg", path)
		return Image{}, false
	}
	if base64.StdEncoding.DecodedLen(len(b.Base64)) > MaxImageBytes+3 {
		c.addf("%s.base64: image larger than %d bytes", path, MaxImageBytes)
		return Image{}, false
	}
	data, err := base64.StdEncoding.DecodeString(b.Base64)
	if err != nil {
		c.addf("%s.base64: not valid standard base64", path)
		return Image{}, false
	}
	if len(data) > MaxImageBytes {
		c.addf("%s.base64: image larger than %d bytes", path, MaxImageBytes)
		return Image{}, false
	}
	if !bytes.HasPrefix(data, sig) {
		c.addf("%s.base64: content is not a %s image", path, b.ContentType)
		return Image{}, false
	}
	b.Base64 = ""
	return Image{ContentType: b.ContentType, Bytes: data}, true
}
