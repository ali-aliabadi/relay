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
)
