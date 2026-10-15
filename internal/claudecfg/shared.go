package claudecfg

import "github.com/GujaLomsadze/trk/internal/cfgfile"

// The JSON editing, file safety and progress block are shared with every agent installer.
type Object = cfgfile.Object

var (
	Parse       = cfgfile.Parse
	NewObject   = cfgfile.NewObject
	ApplyBlock  = cfgfile.ApplyBlock
	backup      = cfgfile.Backup
	writeAtomic = cfgfile.WriteAtomic
	ShellQuote  = cfgfile.ShellQuote
)

const (
	BlockBegin = cfgfile.BlockBegin
	BlockEnd   = cfgfile.BlockEnd
	Snippet    = cfgfile.Snippet
)
