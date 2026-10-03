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
)

var allowedFields = map[string]int{
	BlockText:   fText,
	BlockCode:   fText,
	BlockFields: fItems,
	BlockTable:  fColumns | fRows,
	BlockLink:   fText | fURL,
	BlockImage:  fURL | fBase64 | fContentType | fCaption,
}

// onlyFields reports whether b sets no field outside allowed.
func onlyFields(b *Block, allowed int) bool {
	set := 0
	for bit, present := range map[int]bool{
		fText: b.Text != "", fItems: b.Items != nil, fColumns: b.Columns != nil, fRows: b.Rows != nil,
		fURL: b.URL != "", fBase64: b.Base64 != "", fContentType: b.ContentType != "", fCaption: b.Caption != "",
	} {
		if present {
			set |= bit
		}
	}
	return set&^allowed == 0
}
