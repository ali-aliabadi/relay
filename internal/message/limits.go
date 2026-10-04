package message

// Limits enforced at the API boundary. Layouts may truncate further to fit a
// platform (Telegram: 4096 characters per message, 1024 per caption).
const (
	MaxRecipients     = 10
	MaxBlocks         = 20
	MaxImages         = 1
	MaxTableRows      = 50
	MaxTableColumns   = 8
	MaxFields         = 25
	MaxImageBytes     = 5 << 20
	MaxFiles          = 1
	MaxFileBytes      = 5 << 20 // with base64, fits RELAY_MAX_BODY_BYTES and nginx's 8 MB cap
	MaxFilenameLen    = 128
	MaxContentTypeLen = 128
	MaxTitleLen       = 256
	MaxSourceLen      = 64
	MaxTextLen        = 4000
	MaxLabelLen       = 64
	MaxValueLen       = 1024
	MaxCellLen        = 256
	MaxCaptionLen     = 1024
	MaxLinkTextLen    = 64
	MaxURLLen         = 2048
	MaxIdempotencyLen = 128
	MaxOptions        = 10
	MaxOptionLen      = 64
)
