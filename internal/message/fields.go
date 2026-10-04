package message

// field bits, so each block type can say which fields it accepts.
const (
	fText = 1 << iota
	fItems
	fColumns
	fRows
	fURL
	fBase64
	fContentType
	fCaption
	fOptions
	fWebhook
	fFilename
)

var allowedFields = map[string]int{
	BlockText:   fText,
	BlockCode:   fText,
	BlockFields: fItems,
	BlockTable:  fColumns | fRows,
	BlockLink:   fText | fURL,
	BlockImage:  fURL | fBase64 | fContentType | fCaption,
	BlockFile:   fBase64 | fContentType | fCaption | fFilename,

	BlockQuestion: fText | fOptions | fWebhook,
}

// onlyFields reports whether b sets no field outside allowed.
func onlyFields(b *Block, allowed int) bool {
	set := 0
	for bit, present := range map[int]bool{
		fText: b.Text != "", fItems: b.Items != nil, fColumns: b.Columns != nil, fRows: b.Rows != nil,
		fURL: b.URL != "", fBase64: b.Base64 != "", fContentType: b.ContentType != "", fCaption: b.Caption != "",
		fOptions: b.Options != nil, fWebhook: b.Webhook != "", fFilename: b.Filename != "",
	} {
		if present {
			set |= bit
		}
	}
	return set&^allowed == 0
}
